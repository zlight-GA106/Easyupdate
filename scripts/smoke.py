"""Run an isolated HTTP smoke test with a real APK (Python standard library)."""
import argparse
import hashlib
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

            status, page, _ = request("/login", {"username": "admin", "password": "admin", "csrf": csrf(page)})
            assert status == 200, page.decode()
            token = csrf(page)
            status, page, _ = request("/admin/apps/new", {"csrf": token, "name": "HelloWorld", "package_name": "com.example.helloworld"})
            assert status == 200, page.decode()
            apk = (root / "internal/storage/testdata/helloworld.apk").read_bytes()
            boundary = "easyupdate-smoke-boundary"
            body = (f'--{boundary}\r\nContent-Disposition: form-data; name="csrf"\r\n\r\n{token}\r\n'
                    f'--{boundary}\r\nContent-Disposition: form-data; name="apk"; filename="hello.apk"\r\n'
                    'Content-Type: application/vnd.android.package-archive\r\n\r\n').encode() + apk + f"\r\n--{boundary}--\r\n".encode()
            status, page, _ = request("/admin/apps/1/upload", body, {"Content-Type": f"multipart/form-data; boundary={boundary}"})
            assert status == 200, page.decode()
            upload = re.search(rb'name="upload_token" value="([a-f0-9]+)"', page).group(1).decode()
            name = re.search(rb'name="version_name" value="([^"]+)"', page).group(1).decode()
            assert b'value="com.example.helloworld"' in page and b'readonly' in page
            digest = hashlib.sha256(apk).hexdigest()
            assert digest.encode() in page
            status, page, _ = request("/admin/apps/1/releases", {"csrf": token, "upload_token": upload, "package_name": "com.example.helloworld", "version_name": name, "version_code": 1, "release_notes": "Smoke test"})
            assert status == 200 and digest.encode() in page, page.decode()
            status, page, _ = request("/admin/releases/1/publish", {"csrf": token, "action": "publish"})
            assert status == 200 and "已发布".encode() in page, page.decode()
            print("PASS login / app create / real APK metadata / SHA256 / draft / publish")

            if args.api:
                status, body, _ = request("/api/v1/apps/com.example.helloworld/latest?version_code=0")
                latest = json.loads(body)
                assert status == 200 and latest["update_available"] and latest["version_code"] == 1 and latest["sha256"] == digest
                status, body, _ = request("/api/v1/apps/com.example.helloworld/latest?version_code=1")
                assert status == 200 and not json.loads(body)["update_available"]
                path = "/api/v1/apps/com.example.helloworld/releases/1/download"
                status, body, headers = request(path)
                assert status == 200 and hashlib.sha256(body).hexdigest() == digest
                assert headers["Content-Type"] == "application/vnd.android.package-archive"
                status, body, headers = request(path, headers={"Range": "bytes=0-31"})
                assert status == 206 and body == apk[:32] and headers["Content-Range"].startswith("bytes 0-31/")
                request("/admin/releases/1/publish", {"csrf": token, "action": "unpublish"})
                status, body, _ = request(path)
                assert status == 404
                status, body, _ = request("/api/v1/apps/com.example.helloworld/latest")
                assert status == 404 and json.loads(body)["error"] == "no_release"
                print("PASS latest comparison / streaming download / Range / unpublish")

            if args.extras:
                payload = {"device_id": "a94187b3-cc2f-4ef0-93fb-04e7e3444342", "package_name": "com.example.helloworld", "version_name": "1.0", "version_code": 1}
                for code in (1, 2):
                    payload["version_code"] = code
                    status, body, _ = request("/api/v1/heartbeat", json.dumps(payload).encode(), {"Content-Type": "application/json"})
                    assert status == 200 and json.loads(body)["ok"]
                status, page, _ = request("/admin/devices?app_id=1")
                assert status == 200 and page.count(payload["device_id"].encode()) == 1
                status, page, _ = request("/admin/announcements/new", {"csrf": token, "title": "Test notice", "content": "Hello", "published": "on"})
                assert status == 200
                status, body, _ = request("/api/v1/announcements")
                assert status == 200 and json.loads(body)["items"][0]["title"] == "Test notice"
                print("PASS heartbeat upsert / device filter / announcements")
        except Exception:
            log.flush()
            print((work / "server.log").read_text(errors="replace"))
            raise
        finally:
            process.terminate()
            process.wait(timeout=15)
