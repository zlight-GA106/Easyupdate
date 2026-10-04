package database

import (
	"context"
	"testing"
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
