//go:build windows
// +build windows

package tea

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/erikgeiser/coninput"
	localereader "github.com/mattn/go-localereader"
	"github.com/muesli/cancelreader"
)

func readInputs(ctx context.Context, msgs chan<- Msg, input io.Reader) error {
	if coninReader, ok := input.(*conInputReader); ok {
		return readConInputs(ctx, msgs, coninReader)
	}

	return readAnsiInputs(ctx, msgs, localereader.NewReader(input))
}

func readConInputs(ctx context.Context, msgsch chan<- Msg, con *conInputReader) error {
	var ps coninput.ButtonState                 // keep track of previous mouse state
	var ws coninput.WindowBufferSizeEventRecord // keep track of the last window size event
	for {
		events, err := peekAndReadConsInput(con)
		if err != nil {
			return err
		}
		for _, event := range events {
			var msgs []Msg
			switch e := event.Unwrap().(type) {
			case coninput.KeyEventRecord:
				msgs = keyEventMsgs(e) // Termail patch: see key_conin.go
			case coninput.WindowBufferSizeEventRecord:
				if e != ws {
					ws = e
					msgs = append(msgs, WindowSizeMsg{
						Width:  int(e.Size.X),
						Height: int(e.Size.Y),
					})
				}
			case coninput.MouseEventRecord:
				event := mouseEvent(ps, e)
				if event.Type != MouseUnknown {
					msgs = append(msgs, event)
				}
				ps = e.ButtonState
			case coninput.FocusEventRecord, coninput.MenuEventRecord:
				// ignore
			default: // unknown event
				continue
			}

			// Send all messages to the channel
			for _, msg := range msgs {
				select {
				case msgsch <- msg:
				case <-ctx.Done():
					err := ctx.Err()
					if err != nil {
						return fmt.Errorf("coninput context error: %w", err)
					}
					return nil
				}
			}
		}
	}
}

// Peek for new input in a tight loop and then read the input.
// windows.CancelIo* does not work reliably so peek first and only use the data if
// the console input is not cancelled.
func peekAndReadConsInput(con *conInputReader) ([]coninput.InputRecord, error) {
	events, err := peekConsInput(con)
	if err != nil {
		return events, err
	}
	events, err = coninput.ReadNConsoleInputs(con.conin, intToUint32OrDie(len(events)))
	if con.isCanceled() {
		return events, cancelreader.ErrCanceled
	}
	if err != nil {
		return events, fmt.Errorf("read coninput events: %w", err)
	}
	return events, nil
}

// Convert i to unit32 or panic if it cannot be converted. Check satisfies lint G115.
func intToUint32OrDie(i int) uint32 {
	if i < 0 {
		panic("cannot convert numEvents " + fmt.Sprint(i) + " to uint32")
	}
	return uint32(i) //nolint:gosec
}

// Keeps peeking until there is data or the input is cancelled.
func peekConsInput(con *conInputReader) ([]coninput.InputRecord, error) {
	for {
		events, err := coninput.PeekNConsoleInputs(con.conin, 16)
		if con.isCanceled() {
			return events, cancelreader.ErrCanceled
		}
		if err != nil {
			return events, fmt.Errorf("peek coninput events: %w", err)
		}
		if len(events) > 0 {
			return events, nil
		}
		// Sleep for a bit to avoid busy waiting.
		time.Sleep(16 * time.Millisecond)
	}
}

func mouseEventButton(p, s coninput.ButtonState) (button MouseButton, action MouseAction) {
	btn := p ^ s
	action = MouseActionPress
	if btn&s == 0 {
		action = MouseActionRelease
	}

	if btn == 0 {
		switch {
		case s&coninput.FROM_LEFT_1ST_BUTTON_PRESSED > 0:
			button = MouseButtonLeft
		case s&coninput.FROM_LEFT_2ND_BUTTON_PRESSED > 0:
			button = MouseButtonMiddle
		case s&coninput.RIGHTMOST_BUTTON_PRESSED > 0:
			button = MouseButtonRight
		case s&coninput.FROM_LEFT_3RD_BUTTON_PRESSED > 0:
			button = MouseButtonBackward
		case s&coninput.FROM_LEFT_4TH_BUTTON_PRESSED > 0:
			button = MouseButtonForward
		}
		return button, action
	}

	switch btn {
	case coninput.FROM_LEFT_1ST_BUTTON_PRESSED: // left button
		button = MouseButtonLeft
	case coninput.RIGHTMOST_BUTTON_PRESSED: // right button
		button = MouseButtonRight
	case coninput.FROM_LEFT_2ND_BUTTON_PRESSED: // middle button
		button = MouseButtonMiddle
	case coninput.FROM_LEFT_3RD_BUTTON_PRESSED: // unknown (possibly mouse backward)
		button = MouseButtonBackward
	case coninput.FROM_LEFT_4TH_BUTTON_PRESSED: // unknown (possibly mouse forward)
		button = MouseButtonForward
	}

	return button, action
}

func mouseEvent(p coninput.ButtonState, e coninput.MouseEventRecord) MouseMsg {
	ev := MouseMsg{
		X:     int(e.MousePositon.X),
		Y:     int(e.MousePositon.Y),
		Alt:   e.ControlKeyState.Contains(coninput.LEFT_ALT_PRESSED | coninput.RIGHT_ALT_PRESSED),
		Ctrl:  e.ControlKeyState.Contains(coninput.LEFT_CTRL_PRESSED | coninput.RIGHT_CTRL_PRESSED),
		Shift: e.ControlKeyState.Contains(coninput.SHIFT_PRESSED),
	}
	switch e.EventFlags {
	case coninput.CLICK, coninput.DOUBLE_CLICK:
		ev.Button, ev.Action = mouseEventButton(p, e.ButtonState)
		if ev.Action == MouseActionRelease {
			ev.Type = MouseRelease
		}
		switch ev.Button { //nolint:exhaustive
		case MouseButtonLeft:
			ev.Type = MouseLeft
		case MouseButtonMiddle:
			ev.Type = MouseMiddle
		case MouseButtonRight:
			ev.Type = MouseRight
		case MouseButtonBackward:
			ev.Type = MouseBackward
		case MouseButtonForward:
			ev.Type = MouseForward
		}
	case coninput.MOUSE_WHEELED:
		if e.WheelDirection > 0 {
			ev.Button = MouseButtonWheelUp
			ev.Type = MouseWheelUp
		} else {
			ev.Button = MouseButtonWheelDown
			ev.Type = MouseWheelDown
		}
	case coninput.MOUSE_HWHEELED:
		if e.WheelDirection > 0 {
			ev.Button = MouseButtonWheelRight
			ev.Type = MouseWheelRight
		} else {
			ev.Button = MouseButtonWheelLeft
			ev.Type = MouseWheelLeft
		}
	case coninput.MOUSE_MOVED:
		ev.Button, _ = mouseEventButton(p, e.ButtonState)
		ev.Action = MouseActionMotion
		ev.Type = MouseMotion
	}

	return ev
}
