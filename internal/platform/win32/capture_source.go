package win32

import "strings"

// sourceWindowInfo separates the stable capture boundary from the temporary
// state used by the source menu. A selected game can be hidden or cloaked while
// another desktop is active without becoming a service window.
type sourceWindowInfo struct {
	className                   string
	style, extendedStyle        uintptr
	shellWindow                 bool
	visible, owned, cloaked     bool
	processID, currentProcessID uint32
	title                       string
}

func (w sourceWindowInfo) captureAllowed() bool {
	const (
		child      = 0x40000000 // WS_CHILD
		toolWindow = 0x00000080 // WS_EX_TOOLWINDOW
		noActivate = 0x08000000 // WS_EX_NOACTIVATE
	)
	if w.className == "" || w.shellWindow || w.style&child != 0 || w.extendedStyle&(toolWindow|noActivate) != 0 {
		return false
	}
	// Class names are not translated. Do not blacklist explorer.exe: its
	// ordinary folder windows are valid sources, unlike these shell surfaces.
	// WS_POPUP, WS_EX_TOPMOST and the absence of a caption remain allowed so
	// borderless games are not mistaken for tool windows.
	switch strings.ToLower(w.className) {
	case "progman", "workerw", "shell_traywnd", "shell_secondarytraywnd",
		"notifyiconoverflowwindow", "toplevelwindowforoverflowxamlisland",
		"lumatape.pointerprojection", "lumatape.control", "lumatape.settings":
		return false
	}
	return true
}

func (w sourceWindowInfo) listed() bool {
	return w.captureAllowed() && w.visible && !w.owned && !w.cloaked &&
		w.processID != 0 && w.processID != w.currentProcessID && w.title != ""
}
