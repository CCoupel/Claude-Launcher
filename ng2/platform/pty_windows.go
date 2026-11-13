package platform

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ConPTY (Windows 10 1809+) : CreatePseudoConsole entre deux tubes, processus
// créé avec l'attribut PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE. Appels directs de
// golang.org/x/sys/windows (déjà une dépendance) : pas de bibliothèque de plus.

var procUpdateProcThreadAttribute = windows.NewLazySystemDLL("kernel32.dll").NewProc("UpdateProcThreadAttribute")

type winTerm struct {
	hpc     windows.Handle
	in, out *os.File
	proc    windows.Handle
	pid     int
	attrs   *windows.ProcThreadAttributeListContainer
	once    sync.Once
	werr    error
	done    chan struct{}
	closeMu sync.Mutex
	closed  bool
}

// StartTerminal : commande lancée dans une pseudo-console.
func StartTerminal(s TermSpec) (Terminal, error) {
	if len(s.Argv) == 0 {
		return nil, ErrNoCommand
	}
	app, err := exec.LookPath(s.Argv[0])
	if err != nil {
		return nil, err
	}
	cols, rows := termSize(s.Cols, s.Rows)

	var inR, inW, outR, outW windows.Handle
	if err := windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, fmt.Errorf("conpty : tube d'entrée : %w", err)
	}
	if err := windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		windows.CloseHandle(inR)
		windows.CloseHandle(inW)
		return nil, fmt.Errorf("conpty : tube de sortie : %w", err)
	}
	var hpc windows.Handle
	err = windows.CreatePseudoConsole(windows.Coord{X: int16(cols), Y: int16(rows)}, inR, outW, 0, &hpc)
	// la pseudo-console a dupliqué ses côtés des tubes
	windows.CloseHandle(inR)
	windows.CloseHandle(outW)
	if err != nil {
		windows.CloseHandle(inW)
		windows.CloseHandle(outR)
		return nil, fmt.Errorf("conpty : CreatePseudoConsole : %w", err)
	}
	t := &winTerm{hpc: hpc, in: os.NewFile(uintptr(inW), "conpty-in"), out: os.NewFile(uintptr(outR), "conpty-out"), done: make(chan struct{})}
	if err := t.spawn(app, s); err != nil {
		t.Close()
		return nil, err
	}
	return t, nil
}

func (t *winTerm) spawn(app string, s TermSpec) error {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return fmt.Errorf("conpty : attributs : %w", err)
	}
	t.attrs = attrs
	if r, _, e := procUpdateProcThreadAttribute.Call(uintptr(unsafe.Pointer(attrs.List())), 0,
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, uintptr(t.hpc), unsafe.Sizeof(t.hpc), 0, 0); r == 0 {
		return fmt.Errorf("conpty : UpdateProcThreadAttribute : %w", e)
	}
	si := windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(si))
	// sans cela, des entrées/sorties standard redirigées du launcher seraient
	// héritées à la place de la pseudo-console
	si.Flags |= windows.STARTF_USESTDHANDLES

	appW, err := windows.UTF16PtrFromString(app)
	if err != nil {
		return err
	}
	cmdW, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(s.Argv))
	if err != nil {
		return err
	}
	env := s.Env
	if env == nil {
		env = os.Environ()
	}
	block := envBlock(env)
	var dirW *uint16
	if s.Dir != "" {
		if dirW, err = windows.UTF16PtrFromString(s.Dir); err != nil {
			return err
		}
	}
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(appW, cmdW, nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		&block[0], dirW, &si.StartupInfo, &pi); err != nil {
		return fmt.Errorf("conpty : CreateProcess %s : %w", app, err)
	}
	windows.CloseHandle(pi.Thread)
	t.proc, t.pid = pi.Process, int(pi.ProcessId)
	return nil
}

func (t *winTerm) Read(p []byte) (int, error)  { return t.out.Read(p) }
func (t *winTerm) Write(p []byte) (int, error) { return t.in.Write(p) }
func (t *winTerm) Pid() int                    { return t.pid }

func (t *winTerm) Resize(cols, rows int) error {
	c, r := termSize(cols, rows)
	return windows.ResizePseudoConsole(t.hpc, windows.Coord{X: int16(c), Y: int16(r)})
}

func (t *winTerm) Wait() error {
	t.once.Do(func() {
		defer close(t.done)
		if t.proc == 0 {
			t.werr = ErrNoCommand
			return
		}
		if _, err := windows.WaitForSingleObject(t.proc, windows.INFINITE); err != nil {
			t.werr = err
			return
		}
		var code uint32
		if err := windows.GetExitCodeProcess(t.proc, &code); err != nil {
			t.werr = err
		} else if code != 0 {
			t.werr = fmt.Errorf("statut de sortie %d", code)
		}
	})
	<-t.done
	return t.werr
}

func (t *winTerm) Close() error {
	t.closeMu.Lock()
	defer t.closeMu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	if t.proc != 0 {
		select {
		case <-t.done:
		default:
			_ = windows.TerminateProcess(t.proc, 1)
		}
	}
	// Avant Windows 11 24H2, ClosePseudoConsole attend que la sortie en attente
	// soit lue : elle est vidée en tâche de fond pendant la fermeture (la
	// lecture s'arrête sur EOF, quand la pseudo-console est fermée).
	drained := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, t.out)
		close(drained)
	}()
	_ = t.in.Close()
	windows.ClosePseudoConsole(t.hpc)
	select { // EOF après la fermeture ; borne si un autre lecteur l'a déjà consommé
	case <-drained:
	case <-time.After(2 * time.Second):
	}
	_ = t.out.Close()
	if t.attrs != nil {
		t.attrs.Delete()
	}
	if t.proc != 0 {
		go func(h windows.Handle) { // après la fin de Wait éventuel
			<-t.done
			windows.CloseHandle(h)
		}(t.proc)
		go func() { _ = t.Wait() }()
	}
	return nil
}
