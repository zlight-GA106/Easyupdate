"""Run an isolated HTTP smoke test with a real APK (Python standard library)."""
import argparse
import hashlib
import html
import http.cookiejar
import json
import pathlib
import re
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

root = pathlib.Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument("binary")
parser.add_argument("--api", action="store_true")
parser.add_argument("--extras", action="store_true")
parser.add_argument("--crud", action="store_true", help="Verify CRUD and deletion safeguards (includes API/extras)")
args = parser.parse_args()

with tempfile.TemporaryDirectory(prefix="easyupdate-smoke-") as directory:
    work = pathlib.Path(directory)
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    origin = f"http://127.0.0.1:{port}"
    config = work / "config.yaml"
    config.write_text(f'''server:
  listen: "127.0.0.1:{port}"
  public_url: "{origin}"
database:
  path: {json.dumps(str(work / "data/server.db"))}
storage:
  path: {json.dumps(str(work / "data/apks"))}
  max_upload_mb: 4
admin:
  username: "admin"
  password: "admin"
github:
  token: ""
''', encoding="utf-8")
    with (work / "server.log").open("wb") as log:
        process = subprocess.Popen([str(pathlib.Path(args.binary).resolve()), "-config", str(config)], stdout=log, stderr=log)
        try:
            client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

            def request(path, data=None, headers=None):
                if isinstance(data, dict):
                    data = urllib.parse.urlencode(data).encode()
                req = urllib.request.Request(origin + path, data=data, headers=headers or {})
                try:
                    with client.open(req, timeout=30) as response:
                        return response.status, response.read(), response.headers
                except urllib.error.HTTPError as error:
                    return error.code, error.read(), error.headers

            for _ in range(100):
                try:
                    status, page, _ = request("/login")
                    break
                except urllib.error.URLError:
                    if process.poll() is not None:
                        raise RuntimeError("server exited before startup")
                    time.sleep(0.1)
            else:
                raise RuntimeError("server startup timed out")

            def csrf(page):
                return re.search(rb'name="csrf" value="([a-f0-9]+)"', page).group(1).decode()

            def input_value(page, field):
                match = re.search(rb'<input\b[^>]*\bname="' + re.escape(field.encode()) + rb'"[^>]*\bvalue="([^"]*)"', page)
                assert match, f"missing input value: {field}"
                return html.unescape(match.group(1).decode())

            def entity_id(page, collection):
                match = re.search(fr'(?:href|action)="/admin/{collection}/(\d+)/(?:edit|delete|publish)(?:[?#][^"]*)?"'.encode(), page)
                assert match, f"missing {collection} record link/form"
                return int(match.group(1))

            def row_id(page, collection, marker):
                rows = [row for row in re.findall(rb'<tr\b[^>]*>.*?</tr>', page, re.S) if html.escape(marker).encode() in row]
                assert len(rows) == 1, f"expected one {collection} row for {marker}, got {len(rows)}"
                return entity_id(rows[0], collection)

            def require_links(page, *paths):
                for path in paths:
                    assert f'href="{path}"'.encode() in page, f"missing action link: {path}"

            status, page, _ = request("/login", {"username": "admin", "password": "admin", "csrf": csrf(page)})
            assert status == 200, page.decode()
            token = csrf(page)

            def create_app(name, package):
                status, page, _ = request("/admin/apps/new", {"csrf": token, "name": name, "package_name": package})
                assert status == 200, page.decode()
                return entity_id(page, "apps")

            package = "com.example.helloworld"
            app_id = create_app("HelloWorld", package)
            apk = (root / "internal/storage/testdata/helloworld.apk").read_bytes()
            digest = hashlib.sha256(apk).hexdigest()

            def upload_release(target_app, notes="Smoke test"):
                boundary = "easyupdate-smoke-boundary"
                body = (f'--{boundary}\r\nContent-Disposition: form-data; name="csrf"\r\n\r\n{token}\r\n'
                        f'--{boundary}\r\nContent-Disposition: form-data; name="apk"; filename="hello.apk"\r\n'
                        'Content-Type: application/vnd.android.package-archive\r\n\r\n').encode() + apk + f"\r\n--{boundary}--\r\n".encode()
                status, page, _ = request(f"/admin/apps/{target_app}/upload", body, {"Content-Type": f"multipart/form-data; boundary={boundary}"})
                assert status == 200, page.decode()
                assert input_value(page, "package_name") == package and b'readonly' in page
                assert digest.encode() in page
                version_name = input_value(page, "version_name")
                version_code = int(input_value(page, "version_code"))
                status, page, _ = request(f"/admin/apps/{target_app}/releases", {
                    "csrf": token, "upload_token": input_value(page, "upload_token"), "action": "create",
                    "package_name": package, "version_name": version_name, "version_code": version_code, "release_notes": notes,
                })
                assert status == 200 and digest.encode() in page, page.decode()
                return entity_id(page, "releases"), version_code

            release_id, version_code = upload_release(app_id)
            status, page, _ = request(f"/admin/releases/{release_id}/publish", {"csrf": token, "action": "publish"})
            assert status == 200 and "已发布".encode() in page, page.decode()
            print("PASS login / app create / real APK metadata / SHA256 / draft / publish")

            if args.api or args.crud:
                status, body, _ = request(f"/api/v1/apps/{package}/latest?version_code=0")
                latest = json.loads(body)
                assert status == 200 and latest["update_available"] and latest["version_code"] == version_code and latest["sha256"] == digest
                status, body, _ = request(f"/api/v1/apps/{package}/latest?version_code={version_code}")
                assert status == 200 and not json.loads(body)["update_available"]
                path = f"/api/v1/apps/{package}/releases/{version_code}/download"
                status, body, headers = request(path)
                assert status == 200 and hashlib.sha256(body).hexdigest() == digest
                assert headers["Content-Type"] == "application/vnd.android.package-archive"
                status, body, headers = request(path, headers={"Range": "bytes=0-31"})
                assert status == 206 and body == apk[:32] and headers["Content-Range"].startswith("bytes 0-31/")
                request(f"/admin/releases/{release_id}/publish", {"csrf": token, "action": "unpublish"})
                status, body, _ = request(path)
                assert status == 404
                status, body, _ = request(f"/api/v1/apps/{package}/latest")
                assert status == 404 and json.loads(body)["error"] == "no_release"
                print("PASS latest comparison / streaming download / Range / unpublish")

            if args.extras or args.crud:
                payload = {"device_id": "a94187b3-cc2f-4ef0-93fb-04e7e3444342", "package_name": package, "version_name": "1.0", "version_code": 1}
                for code in (1, 2):
                    payload["version_code"] = code
                    status, body, _ = request("/api/v1/heartbeat", json.dumps(payload).encode(), {"Content-Type": "application/json"})
                    assert status == 200 and json.loads(body)["ok"]
                status, page, _ = request(f"/admin/devices?app_id={app_id}")
                assert status == 200 and page.count(payload["device_id"].encode()) == 1
                status, page, _ = request("/admin/announcements/new", {"csrf": token, "title": "Test notice", "content": "Hello", "published": "on"})
                assert status == 200
                if args.crud:
                    announcement_id = row_id(page, "announcements", "Test notice")
                status, body, _ = request("/api/v1/announcements")
                assert status == 200 and json.loads(body)["items"][0]["title"] == "Test notice"
                print("PASS heartbeat upsert / device filter / announcements")

            if args.crud:
                status, page, _ = request("/admin/apps")
                assert status == 200
                require_links(page, f"/admin/apps/{app_id}", f"/admin/apps/{app_id}/edit", f"/admin/apps/{app_id}/delete")
                status, page, _ = request(f"/admin/apps/{app_id}/edit")
                assert status == 200 and input_value(page, "name") == "HelloWorld"
                status, page, _ = request(f"/admin/apps/{app_id}/edit", {
                    "csrf": token, "name": "HelloWorld Edited", "package_name": package, "description": "CRUD description",
                })
                assert status == 200 and b"HelloWorld Edited" in page and b"CRUD description" in page, page.decode()
                require_links(page, f"/admin/releases/{release_id}", f"/admin/releases/{release_id}#edit", f"/admin/releases/{release_id}/delete")
                status, page, _ = request(f"/admin/apps/{app_id}/edit")
                assert status == 200 and input_value(page, "name") == "HelloWorld Edited" and b"CRUD description" in page

                status, page, _ = request(f"/admin/releases/{release_id}/edit", {
                    "csrf": token, "release_notes": "Edited release notes", "mandatory": "on",
                })
                assert status == 200 and b'id="edit"' in page and b"Edited release notes" in page, page.decode()
                request(f"/admin/releases/{release_id}/publish", {"csrf": token, "action": "publish"})
                status, body, _ = request(f"/api/v1/apps/{package}/latest")
                latest = json.loads(body)
                assert status == 200 and latest["release_notes"] == "Edited release notes" and latest["mandatory"]
                status, page, _ = request(f"/admin/releases/{release_id}/edit", {
                    "csrf": token, "release_notes": "Edited optional release",
                })
                assert status == 200, page.decode()
                status, body, _ = request(f"/api/v1/apps/{package}/latest")
                assert status == 200 and not json.loads(body)["mandatory"]
                apk_path = work / "data/apks" / str(app_id) / str(version_code) / "app.apk"
                assert apk_path.is_file() and hashlib.sha256(apk_path.read_bytes()).hexdigest() == digest
                status, page, _ = request(f"/admin/releases/{release_id}/delete")
                assert status == 200 and apk_path.is_file(), page.decode()
                assert request(f"/admin/releases/{release_id}")[0] == 200
                status, page, _ = request(f"/admin/releases/{release_id}/delete", {"csrf": token, "confirmation": "wrong-version"})
                assert status == 400 and apk_path.is_file(), page.decode()
                assert request(f"/admin/releases/{release_id}")[0] == 200
                status, page, _ = request(f"/admin/releases/{release_id}/delete", {"csrf": "invalid", "confirmation": version_code})
                assert status == 403 and apk_path.is_file(), page.decode()
                assert request(f"/admin/releases/{release_id}")[0] == 200
                status, page, _ = request(f"/admin/releases/{release_id}/delete", {"csrf": token, "confirmation": version_code})
                assert status == 200 and not apk_path.parent.exists(), page.decode()
                assert request(f"/admin/releases/{release_id}")[0] == 404
                assert request(f"/api/v1/apps/{package}/releases/{version_code}/download")[0] == 404
                print("PASS application edit / version edit / list actions / confirmed version deletion / APK cleanup / CSRF")

                # The SQLite INTEGER PRIMARY KEY may reuse the deleted release ID.
                release_id, version_code = upload_release(app_id, "Cascade release")
                status, page, _ = request(f"/admin/releases/{release_id}/publish", {"csrf": token, "action": "publish"})
                assert status == 200, page.decode()
                apk_path = work / "data/apks" / str(app_id) / str(version_code) / "app.apk"
                assert apk_path.is_file()

                manual_uuid = "0d40f4ce-6cff-4b7a-8cd3-9582e6c35b20"
                edited_uuid = "94cc1c88-8b73-49ee-9c31-4c0a804504f0"
                status, page, _ = request(f"/admin/devices/new?app_id={app_id}")
                assert status == 200 and b'name="device_id"' in page
                status, page, _ = request("/admin/devices/new", {
                    "csrf": token, "device_id": manual_uuid, "app_id": app_id, "version_name": "Manual", "version_code": 3,
                })
                assert status == 200, page.decode()
                device_id = row_id(page, "devices", manual_uuid)
                require_links(page, f"/admin/devices/{device_id}/edit", f"/admin/devices/{device_id}/delete")
                status, page, _ = request(f"/admin/devices/{device_id}/edit")
                assert status == 200 and input_value(page, "device_id") == manual_uuid and input_value(page, "version_code") == "3"
                status, page, _ = request(f"/admin/devices/{device_id}/edit", {
                    "csrf": token, "device_id": edited_uuid, "app_id": app_id, "version_name": "Manual Edited", "version_code": 4,
                })
                assert status == 200 and manual_uuid.encode() not in page, page.decode()
                assert row_id(page, "devices", edited_uuid) == device_id
                status, page, _ = request(f"/admin/devices/{device_id}/edit")
                assert status == 200 and input_value(page, "device_id") == edited_uuid and input_value(page, "version_name") == "Manual Edited" and input_value(page, "version_code") == "4"
                status, page, _ = request(f"/admin/devices/{device_id}/delete")
                assert status == 200 and edited_uuid.encode() in page
                assert request(f"/admin/devices/{device_id}/edit")[0] == 200
                status, page, _ = request(f"/admin/devices/{device_id}/delete", {"csrf": token, "confirmation": payload["device_id"]})
                assert status == 400 and request(f"/admin/devices/{device_id}/edit")[0] == 200, page.decode()
                status, page, _ = request(f"/admin/devices/{device_id}/delete", {"csrf": token, "confirmation": edited_uuid})
                assert status == 200 and edited_uuid.encode() not in page and payload["device_id"].encode() in page, page.decode()
                assert request(f"/admin/devices/{device_id}/edit")[0] == 404
                print("PASS manual device create / edit / action links / UUID-confirmed deletion")

                status, page, _ = request("/admin/announcements")
                assert status == 200
                require_links(page, f"/admin/announcements/{announcement_id}/edit", f"/admin/announcements/{announcement_id}/delete")
                status, page, _ = request(f"/admin/announcements/{announcement_id}/edit")
                assert status == 200 and input_value(page, "title") == "Test notice"
                status, page, _ = request(f"/admin/announcements/{announcement_id}/edit", {
                    "csrf": token, "title": "Edited notice", "content": "Edited public content", "published": "on",
                })
                assert status == 200, page.decode()
                status, body, _ = request("/api/v1/announcements")
                items = json.loads(body)["items"]
                assert status == 200 and any(item["id"] == announcement_id and item["title"] == "Edited notice" and item["content"] == "Edited public content" for item in items)
                status, page, _ = request(f"/admin/announcements/{announcement_id}/delete")
                assert status == 200 and request(f"/admin/announcements/{announcement_id}/edit")[0] == 200
                status, page, _ = request(f"/admin/announcements/{announcement_id}/delete", {"csrf": token, "confirmation": "wrong-notice"})
                assert status == 400, page.decode()
                assert request(f"/admin/announcements/{announcement_id}/edit")[0] == 200
                status, body, _ = request("/api/v1/announcements")
                assert status == 200 and any(item["id"] == announcement_id for item in json.loads(body)["items"])
                status, page, _ = request(f"/admin/announcements/{announcement_id}/delete", {"csrf": token, "confirmation": announcement_id})
                assert status == 200, page.decode()
                assert request(f"/admin/announcements/{announcement_id}/edit")[0] == 404
                status, body, _ = request("/api/v1/announcements")
                assert status == 200 and not any(item["id"] == announcement_id for item in json.loads(body)["items"])
                print("PASS announcement edit / public feed / action links / confirmed deletion")

                other_package = "com.example.keep"
                other_app_id = create_app("KeepApp", other_package)
                keep_uuid = "27b23699-f0dc-4fc5-b8c8-909bb9d1767a"
                keep_payload = {"device_id": keep_uuid, "package_name": other_package, "version_name": "Keep", "version_code": 1}
                status, body, _ = request("/api/v1/heartbeat", json.dumps(keep_payload).encode(), {"Content-Type": "application/json"})
                assert status == 200 and json.loads(body)["ok"]
                status, page, _ = request("/admin/announcements/new", {"csrf": token, "title": "Keep notice", "content": "Keep content", "published": "on"})
                assert status == 200, page.decode()
                keep_announcement_id = row_id(page, "announcements", "Keep notice")

                status, page, _ = request(f"/admin/apps/{app_id}/delete")
                assert status == 200 and package.encode() in page and apk_path.is_file(), page.decode()
                assert request(f"/admin/apps/{app_id}")[0] == 200
                status, page, _ = request(f"/admin/apps/{app_id}/delete", {"csrf": token, "confirmation": other_package})
                assert status == 400 and apk_path.is_file(), page.decode()
                assert request(f"/admin/apps/{app_id}")[0] == 200
                status, page, _ = request(f"/admin/devices?app_id={app_id}")
                assert status == 200 and payload["device_id"].encode() in page
                status, page, _ = request(f"/admin/apps/{app_id}/delete", {"csrf": token, "confirmation": package})
                assert status == 200 and not (work / "data/apks" / str(app_id)).exists(), page.decode()
                assert request(f"/admin/apps/{app_id}")[0] == 404
                assert request(f"/admin/releases/{release_id}")[0] == 404
                for path in (f"/api/v1/apps/{package}/latest", f"/api/v1/apps/{package}/releases/{version_code}/download"):
                    status, body, _ = request(path)
                    assert status == 404 and json.loads(body)["error"] == "app_not_found"
                status, page, _ = request("/admin/devices")
                assert status == 200 and payload["device_id"].encode() not in page and keep_uuid.encode() in page
                status, page, _ = request(f"/admin/apps/{other_app_id}/edit")
                assert status == 200 and input_value(page, "name") == "KeepApp" and input_value(page, "package_name") == other_package
                status, body, _ = request("/api/v1/announcements")
                assert status == 200 and any(item["id"] == keep_announcement_id and item["title"] == "Keep notice" and item["content"] == "Keep content" for item in json.loads(body)["items"])
                print("PASS app cascade deletion / API 404 / APK directory cleanup / unrelated app-device-notice isolation")
        except Exception:
            log.flush()
            print((work / "server.log").read_text(errors="replace"))
            raise
        finally:
            process.terminate()
            process.wait(timeout=15)
