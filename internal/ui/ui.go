package ui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

var (
	Accent = lipgloss.Color("#24a1de")
	Green  = lipgloss.Color("#3fb950")
	Yellow = lipgloss.Color("#d29922")
	Red    = lipgloss.Color("#f85149")
	Muted  = lipgloss.Color("#8b949e")
	Text   = lipgloss.Color("#e6edf3")

	AccentStyle = lipgloss.NewStyle().Foreground(Accent)
	BoldStyle   = lipgloss.NewStyle().Bold(true)
	TitleStyle  = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	OKStyle     = lipgloss.NewStyle().Foreground(Green)
	WarnStyle   = lipgloss.NewStyle().Foreground(Yellow)
	ErrStyle    = lipgloss.NewStyle().Foreground(Red)
	MutedStyle  = lipgloss.NewStyle().Foreground(Muted)
	DoneStyle   = lipgloss.NewStyle().Foreground(Green).Bold(true)
)

type UI struct {
	out io.Writer
}

func New() *UI {
	return &UI{out: colorprofile.NewWriter(os.Stdout, os.Environ())}
}

func NewPlain(w io.Writer) *UI {
	return &UI{out: w}
}

func (u *UI) Writer() io.Writer { return u.out }

func (u *UI) print(s string) { fmt.Fprint(u.out, s) }

func (u *UI) Banner() {
	art := []string{
		"  ████████╗██████╗  █████╗ ███████╗███████╗██╗██╗  ██╗",
		"     ██╔══╝██╔══██╗██╔══██╗██╔════╝██╔════╝██║██║ ██╔╝",
		"     ██║   ██████╔╝███████║█████╗  █████╗  ██║█████╔╝ ",
		"     ██║   ██╔══██╗██╔══██║██╔══╝  ██╔══╝  ██║██╔═██╗ ",
		"     ██║   ██║  ██║██║  ██║███████╗██║     ██║██║  ██╗",
		"     ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝╚══════╝╚═╝     ╚═╝╚═╝  ╚═╝",
		"",
		"                      ◉",
		"                      │",
		"                   ╔═════╗",
		"              ◉ ── ╠     ╣ ── ◉",
		"                   ╚═════╝",
		"                      │",
		"                      ◉",
	}
	u.print("\n" + TitleStyle.Render(strings.Join(art, "\n")) + "\n\n")
	u.print("  " + MutedStyle.Render("+ Traefik Manager - tm") + "\n\n")
}

func (u *UI) Step(msg string) { u.print("\n" + TitleStyle.Render("▸ "+msg) + "\n") }
func (u *UI) OK(msg string)   { u.print("  " + OKStyle.Render("✔") + "  " + msg + "\n") }
func (u *UI) Warn(msg string) { u.print("  " + WarnStyle.Render("⚠") + "  " + msg + "\n") }
func (u *UI) Info(msg string) { u.print("  " + MutedStyle.Render("ℹ  "+msg) + "\n") }
func (u *UI) Error(msg string) {
	u.print("\n  " + ErrStyle.Render("✖  Error: "+msg) + "\n\n")
}
func (u *UI) Sep() {
	u.print("\n  " + MutedStyle.Render("────────────────────────────────────────") + "\n")
}
func (u *UI) Section(name string) {
	u.Sep()
	u.print("\n  " + BoldStyle.Render("-- "+name+" --") + "\n")
}
func (u *UI) Title(msg string) { u.print("\n  " + BoldStyle.Render(msg) + "\n") }
func (u *UI) Blank()           { u.print("\n") }
func (u *UI) Line(format string, args ...any) {
	u.print("  " + fmt.Sprintf(format, args...) + "\n")
}
func (u *UI) Code(cmd string) { u.print("  " + MutedStyle.Render("  "+cmd) + "\n") }
func (u *UI) KV(key, value string) {
	u.print(fmt.Sprintf("  %-20s%s\n", key, value))
}
func (u *UI) KVAccent(key, value string) {
	u.print(fmt.Sprintf("  %-20s%s\n", key, AccentStyle.Render(value)))
}
func (u *UI) KVMuted(key, value string) {
	u.print("  " + MutedStyle.Render(fmt.Sprintf("%-20s%s", key, value)) + "\n")
}
func (u *UI) Heading(msg string) { u.print("\n  " + TitleStyle.Render(msg) + "\n") }
func (u *UI) Done(msg string) {
	bar := strings.Repeat("━", 51)
	u.print("\n" + DoneStyle.Render(bar) + "\n" + DoneStyle.Render("  "+msg) + "\n" + DoneStyle.Render(bar) + "\n\n")
}

func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func Interactive() bool {
	return IsTerminal(os.Stdout) && (IsTerminal(os.Stdin) || ttyAvailable())
}

func ttyAvailable() bool {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

func OpenTTY() (*os.File, error) {
	if IsTerminal(os.Stdin) {
		return os.Stdin, nil
	}
	return os.OpenFile("/dev/tty", os.O_RDWR, 0)
}

func Theme() huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		t := huh.ThemeBase(isDark)
		t.Focused.Base = t.Focused.Base.BorderForeground(Muted)
		t.Focused.Card = t.Focused.Base
		t.Focused.Title = t.Focused.Title.Foreground(Accent).Bold(true)
		t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(Accent).Bold(true).MarginBottom(1)
		t.Focused.Directory = t.Focused.Directory.Foreground(Accent)
		t.Focused.Description = t.Focused.Description.Foreground(Muted)
		t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(Red)
		t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(Red)
		t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(Accent)
		t.Focused.NextIndicator = t.Focused.NextIndicator.Foreground(Accent)
		t.Focused.PrevIndicator = t.Focused.PrevIndicator.Foreground(Accent)
		t.Focused.Option = t.Focused.Option.Foreground(Text)
		t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(Accent)
		t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(Green)
		t.Focused.SelectedPrefix = lipgloss.NewStyle().Foreground(Green).SetString("✔ ")
		t.Focused.UnselectedPrefix = lipgloss.NewStyle().Foreground(Muted).SetString("• ")
		t.Focused.UnselectedOption = t.Focused.UnselectedOption.Foreground(Text)
		t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(lipgloss.Color("#0d1117")).Background(Accent)
		t.Focused.Next = t.Focused.FocusedButton
		t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(Text).Background(lipgloss.Color("#30363d"))
		t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(Green)
		t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(Muted)
		t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(Accent)
		t.Blurred = t.Focused
		t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
		t.Blurred.Card = t.Blurred.Base
		t.Blurred.NextIndicator = lipgloss.NewStyle()
		t.Blurred.PrevIndicator = lipgloss.NewStyle()
		t.Group.Title = t.Focused.Title
		t.Group.Description = t.Focused.Description
		return t
	})
}

func Mask(s string) string {
	if s == "" {
		return "(not set)"
	}
	if len(s) <= 4 {
		return "••••••••"
	}
	return s[:4] + "••••••••"
}
