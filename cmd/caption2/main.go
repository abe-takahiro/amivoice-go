package main

import (
	"encoding/binary"
	"fmt"
	"github.com/gordonklaus/portaudio"
	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
	"github.com/juntaki/amivoice-go"
	"github.com/juntaki/amivoice-go/cmd/lib"
	"html"
	"io"
	"log"
	"os"
	"time"
)

type Caption struct {
	widget gtk.IWidget
	label  *gtk.Label
}

func NewCaption() *Caption {
	f, err := gtk.FixedNew()
	if err != nil {
		log.Fatal("Unable to create fixed:", err)
	}

	bg, err := gtk.LabelNew("")
	if err != nil {
		log.Fatal("Unable to create label:", err)
	}
	bg.SetXAlign(0)
	bg.SetYAlign(0)
	f.Put(bg, 0, 0)
	return &Caption{widget: f, label: bg}
}

func (c *Caption) setMessage(input string) {
	splitLen := 50
	runes := []rune(input)
	lastLineLen := len(runes) % splitLen
	if lastLineLen == 0 {
		lastLineLen = splitLen
	}
	lastLine := runes[len(runes)-lastLineLen:]

	firstLine := []rune("")
	if len(runes)-lastLineLen-splitLen >= 0 {
		firstLine = runes[len(runes)-lastLineLen-splitLen : len(runes)-lastLineLen]
	}

	message := html.EscapeString(string(firstLine) + "\n" + string(lastLine))
	format := `<b><span background="%s" foreground="%s" size="xx-large" lang="ja">%s</span></b>`
	c.label.SetMarkup(fmt.Sprintf(format, "black", "white", message))
}

func main() {
	const appID = "org.gtk.example"
	application, err := gtk.ApplicationNew(appID, glib.APPLICATION_FLAGS_NONE)
	// Check to make sure no errors when creating Gtk Application
	if err != nil {
		log.Fatal("Could not create application.", err)
	}

	application.Connect("activate", func() {
		setting, err := lib.ReadSetting()
		if err != nil {
			log.Fatalf("fatal error: %+v\n", err)
		}

		pr, pw := io.Pipe()

		// PortAudio input with buffer
		err = portaudio.Initialize()
		if err != nil {
			log.Fatalf("fatal error: %+v\n", err)
		}
		defer portaudio.Terminate()
		in := make([]int16, 1024)
		stream, err := portaudio.OpenDefaultStream(1, 0, 16000, len(in), in)
		if err != nil {
			log.Fatalf("fatal error: %+v\n", err)
		}
		defer stream.Close()

		err = stream.Start()
		if err != nil {
			log.Fatalf("fatal error: %+v\n", err)
		}
		go func() {
			for {
				err = stream.Read()
				if err != nil {
					if err == portaudio.InputOverflowed {
						log.Println("Ignore input overflowed")
					} else {
						log.Fatalf("fatal: %+v\n", err)
					}
				}
				err = binary.Write(pw, binary.LittleEndian, in)
				if err != nil {
					log.Fatalf("fatal: %+v\n", err)
				}
			}
		}()

		final := make(chan *amivoice.AEvent)
		progress := make(chan *amivoice.UEvent)

		go func() {
			for {
				retry := make(chan struct{})

				c, err := amivoice.NewConnection(setting.AppKey, setting.NoLog)
				if err != nil {
					return
				}
				go func() {
					err = c.CollectResult(final, progress, nil)
					if err != nil {
						log.Printf("retry: %+v\n", err)
						retry <- struct{}{}
					}
				}()
				go func() {
					err = c.Recognize(setting.GenerateRecognitionConfig(pr))
					if err != nil {
						log.Printf("retry: %+v\n", err)
						retry <- struct{}{}
					}
				}()
				<-retry
				c.Close()
			}
		}()

		// Create ApplicationWindow
		win, err := gtk.ApplicationWindowNew(application)
		if err != nil {
			log.Fatal("Could not create application window.", err)
		}
		win.SetPosition(gtk.WIN_POS_CENTER)
		win.SetTitle("Transcribe")
		win.Connect("destroy", func() {
			gtk.MainQuit()
		})
		win.Connect("button-press-event", func(w *gtk.ApplicationWindow, e *gdk.Event) {
			ev := gdk.EventButton{Event: e}
			if ev.Button() != 1 {
				return
			}
			win.BeginMoveDrag(int(ev.Button()), int(ev.XRoot()), int(ev.YRoot()), uint32(time.Now().Unix()))
		})
		win.Connect("window_state_event", func() {
			win.SetKeepAbove(true)
		})
		win.SetDecorated(false)
		win.SetKeepAbove(true)
		win.SetAppPaintable(true)
		win.ResetStyle()

		lastText := ""
		finalText := ">"
		currentText := ">"

		caption := NewCaption()
		tick := time.NewTicker(500 * time.Millisecond)
		go func() {
			for {
				select {
				case val := <-final:
					finalText += val.Text
					currentText = finalText
				case val := <-progress:
					currentText = finalText + val.Text
				case <-tick.C:
					if lastText != currentText {
						_, err = glib.IdleAdd(caption.setMessage, currentText)
						if err != nil {
							log.Fatalf("fatal error: %+v\n", err)
						}
						lastText = currentText
					}
				}
			}
		}()

		win.Add(caption.widget)
		sc := win.GetScreen()
		v, err := sc.GetRGBAVisual()
		win.SetVisual(v)
		win.SetDefaultSize(10, 10)
		win.ShowAll()
		gtk.Main()
	})

	// Run Gtk application
	application.Run(os.Args)

}
