package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHeartbeatUpsertPreservesFirstSeen(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	id, _ := s.SaveApp(ctx, App{Name: "App", PackageName: "com.example.app"})
	d := Device{DeviceID: "a94187b3-cc2f-4ef0-93fb-04e7e3444342", AppID: id, VersionName: "1.0", VersionCode: 1}
	if err := s.Heartbeat(ctx, d); err != nil {
		t.Fatal(err)
	}
	first, _ := s.Devices(ctx, id, 50, 0)
	d.VersionName = "2.0"
	d.VersionCode = 2
	if err := s.Heartbeat(ctx, d); err != nil {
		t.Fatal(err)
	}
	second, _ := s.Devices(ctx, id, 50, 0)
	if len(second) != 1 || second[0].VersionCode != 2 || second[0].FirstSeen != first[0].FirstSeen || second[0].LastSeen < first[0].LastSeen {
		t.Fatalf("upsert: %+v", second)
	}
}

func TestDeviceEditPreservesSeenTimesAndOtherRecords(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	appID, err := s.SaveApp(ctx, App{Name: "App", PackageName: "com.example.app"})
	if err != nil {
		t.Fatal(err)
	}
	otherAppID, err := s.SaveApp(ctx, App{Name: "Other", PackageName: "com.example.other"})
	if err != nil {
		t.Fatal(err)
	}
	d := Device{DeviceID: "A94187B3-CC2F-4EF0-93FB-04E7E3444342", AppID: appID, VersionName: "1.0", VersionCode: 1, FirstSeen: "untrusted", LastSeen: "untrusted"}
	id, err := s.SaveDevice(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	original, err := s.Device(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if original.DeviceID != strings.ToLower(d.DeviceID) || original.AppName != "App" || original.LastSeen != "" {
		t.Fatalf("created device: %+v", original)
	}
	firstSeen, err := time.Parse(time.RFC3339Nano, original.FirstSeen)
	if err != nil {
		t.Fatalf("new device needs a server timestamp: %v", err)
	}
	otherID, err := s.SaveDevice(ctx, Device{DeviceID: original.DeviceID, AppID: otherAppID, VersionName: "untouched", VersionCode: 1})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Device(ctx, otherID)
	if err != nil {
		t.Fatal(err)
	}
	edited := original
	edited.DeviceID = "b94187b3-cc2f-4ef0-93fb-04e7e3444342"
	edited.AppID = otherAppID
	edited.VersionName = "2.0"
	edited.VersionCode = 2
	edited.FirstSeen = "ignored"
	edited.LastSeen = "ignored"
	if savedID, err := s.SaveDevice(ctx, edited); err != nil || savedID != id {
		t.Fatalf("edit: id=%d err=%v", savedID, err)
	}
	after, err := s.Device(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.DeviceID != edited.DeviceID || after.AppID != otherAppID || after.VersionCode != 2 || after.VersionName != "2.0" || after.FirstSeen != original.FirstSeen || after.LastSeen != original.LastSeen {
		t.Fatalf("edit changed wrong fields: %+v", after)
	}
	if got, err := s.Device(ctx, otherID); err != nil || got != other {
		t.Fatalf("edit affected another record: %+v err=%v", got, err)
	}
	if err := s.Heartbeat(ctx, Device{DeviceID: edited.DeviceID, AppID: otherAppID, VersionName: "3.0", VersionCode: 3}); err != nil {
		t.Fatal(err)
	}
	heartbeat, err := s.Device(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if heartbeat.VersionCode != 3 || heartbeat.VersionName != "3.0" || heartbeat.FirstSeen != original.FirstSeen || heartbeat.LastSeen == "" {
		t.Fatalf("heartbeat after edit: %+v", heartbeat)
	}
	lastSeen, err := time.Parse(time.RFC3339Nano, heartbeat.LastSeen)
	if err != nil || lastSeen.Before(firstSeen) {
		t.Fatalf("heartbeat timestamp: %q err=%v", heartbeat.LastSeen, err)
	}
	if got, err := s.Device(ctx, otherID); err != nil || got != other {
		t.Fatalf("heartbeat affected another record: %+v err=%v", got, err)
	}
}

func TestDeviceConflictsAndMissingForeignKeysDoNotWrite(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	appID, err := s.SaveApp(ctx, App{Name: "App", PackageName: "com.example.app"})
	if err != nil {
		t.Fatal(err)
	}
	first := Device{DeviceID: "a94187b3-cc2f-4ef0-93fb-04e7e3444342", AppID: appID, VersionName: "1", VersionCode: 1}
	first.ID, err = s.SaveDevice(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID = 0
	second.DeviceID = "b94187b3-cc2f-4ef0-93fb-04e7e3444342"
	second.ID, err = s.SaveDevice(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	originalFirst, err := s.Device(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	originalSecond, err := s.Device(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := first
	duplicate.ID = 0
	duplicate.DeviceID = strings.ToUpper(first.DeviceID)
	if _, err := s.SaveDevice(ctx, duplicate); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate create: %v", err)
	}
	duplicate.ID = second.ID
	if _, err := s.SaveDevice(ctx, duplicate); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate edit: %v", err)
	}
	for _, id := range []int64{0, first.ID} {
		missingApp := first
		missingApp.ID = id
		missingApp.AppID = appID + 100
		if _, err := s.SaveDevice(ctx, missingApp); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("missing app: %v", err)
		}
	}
	missingDevice := first
	missingDevice.ID = second.ID + 100
	if _, err := s.SaveDevice(ctx, missingDevice); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing device: %v", err)
	}
	if got, err := s.Device(ctx, first.ID); err != nil || got != originalFirst {
		t.Fatalf("rejected write affected first device: %+v err=%v", got, err)
	}
	if got, err := s.Device(ctx, second.ID); err != nil || got != originalSecond {
		t.Fatalf("rejected write affected second device: %+v err=%v", got, err)
	}
	items, err := s.Devices(ctx, 0, 10, 0)
	if err != nil || len(items) != 2 {
		t.Fatalf("rejected write added a record: %+v err=%v", items, err)
	}
}

func TestDeviceDeleteRequiresCurrentUUIDAndHeartbeatCanRecreate(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	appID, err := s.SaveApp(ctx, App{Name: "App", PackageName: "com.example.app"})
	if err != nil {
		t.Fatal(err)
	}
	d := Device{DeviceID: "a94187b3-cc2f-4ef0-93fb-04e7e3444342", AppID: appID, VersionName: "1", VersionCode: 1}
	d.ID, err = s.SaveDevice(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	oldUUID := d.DeviceID
	d.DeviceID = "b94187b3-cc2f-4ef0-93fb-04e7e3444342"
	if _, err := s.SaveDevice(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDevice(ctx, d.ID, oldUUID); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale confirmation: %v", err)
	}
	if _, err := s.Device(ctx, d.ID); err != nil {
		t.Fatalf("stale confirmation deleted device: %v", err)
	}
	if err := s.DeleteDevice(ctx, d.ID, d.DeviceID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Device(ctx, d.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("delete did not remove record: %v", err)
	}
	if err := s.DeleteDevice(ctx, d.ID, d.DeviceID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("repeat delete: %v", err)
	}
	d.VersionName = "2"
	d.VersionCode = 2
	if err := s.Heartbeat(ctx, d); err != nil {
		t.Fatal(err)
	}
	items, err := s.Devices(ctx, appID, 10, 0)
	if err != nil || len(items) != 1 || items[0].DeviceID != d.DeviceID || items[0].VersionCode != 2 {
		t.Fatalf("heartbeat recreation: %+v err=%v", items, err)
	}
}
