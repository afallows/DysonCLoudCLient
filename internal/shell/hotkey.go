package shell

import (
	"golang.org/x/term"
	"os"
)

// ListenForCtrlX listens for the CTRL+X hotkey and sends os.Interrupt
// on the provided signal channel when pressed.
func ListenForCtrlX(sig chan<- os.Signal) {
	go func() {
		fd := int(os.Stdin.Fd())
		oldState, err := term.MakeRaw(fd)
		if err != nil {
			return
		}
		defer term.Restore(fd, oldState)

		buf := make([]byte, 1)
		for {
			if _, err := os.Stdin.Read(buf); err == nil {
				if buf[0] == 24 { // CTRL+X
					sig <- os.Interrupt
					return
				}
			} else {
				return
			}
		}
	}()
}
