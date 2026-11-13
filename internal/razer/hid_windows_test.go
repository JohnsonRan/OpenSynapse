package razer

import (
	"errors"
	"testing"
)

// A Device from an older scan may still be in a round's hands after the
// rescan closed it. It must not open its collection again: nothing closes
// that handle. The path names no device, so nothing is opened here either.
func TestClosedConnRefuses(t *testing.T) {
	c := &conn{path: `\\?\hid#vid_1532&pid_ffff#closed-conn-test`}
	d := &Device{Model: &Model{tx: map[string]byte{}}, txOK: map[string]byte{}, candidates: []*conn{c}}
	d.Close()
	if _, err := d.run("test", 0x00, 0x81, 0x02); !errors.Is(err, ErrClosed) {
		t.Fatalf("request on a closed device: %v, want ErrClosed", err)
	}
	if c.h != 0 {
		t.Fatal("a closed device opened a handle")
	}
}
