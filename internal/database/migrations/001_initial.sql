CREATE TABLE apps (
 id INTEGER PRIMARY KEY,
 name TEXT NOT NULL,
 package_name TEXT NOT NULL UNIQUE,
 description TEXT NOT NULL DEFAULT '',
 icon_path TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE TABLE releases (
 id INTEGER PRIMARY KEY,
 app_id INTEGER NOT NULL REFERENCES apps(id) ON DELETE RESTRICT,
 version_name TEXT NOT NULL,
 version_code INTEGER NOT NULL CHECK(version_code > 0),
 release_notes TEXT NOT NULL DEFAULT '',
 apk_filename TEXT NOT NULL,
 apk_path TEXT NOT NULL,
 apk_size INTEGER NOT NULL,
 apk_sha256 TEXT NOT NULL,
 mandatory INTEGER NOT NULL DEFAULT 0 CHECK(mandatory IN (0,1)),
 published INTEGER NOT NULL DEFAULT 0 CHECK(published IN (0,1)),
 created_at TEXT NOT NULL,
 published_at TEXT,
 github_release_id INTEGER,
 github_tag TEXT NOT NULL DEFAULT '',
 UNIQUE(app_id, version_code)
);
CREATE INDEX releases_latest ON releases(app_id, published, version_code DESC);
CREATE TABLE devices (
 id INTEGER PRIMARY KEY,
 device_id TEXT NOT NULL,
 app_id INTEGER NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
 version_name TEXT NOT NULL,
 version_code INTEGER NOT NULL CHECK(version_code >= 0),
 first_seen TEXT NOT NULL,
 last_seen TEXT NOT NULL,
 UNIQUE(device_id, app_id)
);
CREATE INDEX devices_last_seen ON devices(last_seen DESC);
CREATE TABLE announcements (
 id INTEGER PRIMARY KEY,
 title TEXT NOT NULL,
 content TEXT NOT NULL,
 published INTEGER NOT NULL DEFAULT 0 CHECK(published IN (0,1)),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE TABLE github_sources (
 id INTEGER PRIMARY KEY,
 app_id INTEGER NOT NULL UNIQUE REFERENCES apps(id) ON DELETE CASCADE,
 owner TEXT NOT NULL,
 repo TEXT NOT NULL,
 enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
 asset_pattern TEXT NOT NULL DEFAULT '*.apk',
 last_checked_at TEXT,
 last_release_id INTEGER
);
PRAGMA user_version = 1;
