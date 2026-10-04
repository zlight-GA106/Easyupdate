package database

type App struct {
	ID                                                             int64
	Name, PackageName, Description, IconPath, CreatedAt, UpdatedAt string
	LatestVersion                                                  string
	ReleaseCount                                                   int
}
type Release struct {
	ID, AppID, VersionCode, APKSize, GitHubReleaseID                                              int64
	VersionName, ReleaseNotes, APKFilename, APKPath, APKSHA256, CreatedAt, PublishedAt, GitHubTag string
	Mandatory, Published                                                                          bool
	AppName, PackageName                                                                          string
}
type Device struct {
	ID, AppID, VersionCode                              int64
	DeviceID, AppName, VersionName, FirstSeen, LastSeen string
}
type Announcement struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Published bool   `json:"-"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"-"`
}
type GitHubSource struct {
	ID, AppID, LastReleaseID                 int64
	Owner, Repo, AssetPattern, LastCheckedAt string
	Enabled                                  bool
}
