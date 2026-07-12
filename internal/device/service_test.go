package device

import (
	"path/filepath"
	"testing"

	"github.com/leganck/wakehub/internal/clientlink"
	"github.com/leganck/wakehub/internal/config"
)

func TestShutdownRequiresBoundClient(t *testing.T) {
	store, err := config.Open(filepath.Join(t.TempDir(), "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	hub := clientlink.NewHub("")
	svc := NewService(store, hub)
	d, err := svc.Create(config.Device{Name: "pc", MAC: "aa:bb:cc:dd:ee:ff"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Shutdown(d.ID)
	if err == nil {
		t.Fatal("want error without bound client")
	}
	if len(svc.Audit().List(10)) < 1 {
		t.Fatal("audit empty")
	}
}

func TestWakeUnknownDevice(t *testing.T) {
	store, err := config.Open(filepath.Join(t.TempDir(), "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(store, clientlink.NewHub(""))
	if err := svc.Wake("nope"); err == nil {
		t.Fatal("want error")
	}
}
