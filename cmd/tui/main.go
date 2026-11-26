// Command tui is a full-screen, Claude-Code-style chat over any llmmesh
// provider, built with Bubble Tea. The message history scrolls in a viewport,
// the input is pinned to the bottom, the active model shows in yellow on the
// status bar, and model tokens stream in live.
//
// Usage:
//
//	export ANTHROPIC_API_KEY=...        # or OPENAI_API_KEY / GOOGLE_AI_STUDIO_KEY
//	go run ./cmd/tui -provider anthropic
//
// Enter sends. Ctrl+C quits. PgUp/PgDn scroll the history.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/wuesty/llmmesh/config"
	"github.com/wuesty/llmmesh/internal/chatkit"
	"github.com/wuesty/llmmesh/llm"
	"github.com/wuesty/llmmesh/streaming"
	"github.com/wuesty/llmmesh/types"
)

const dotEnvPath = ".env"

// Streaming events delivered from the API goroutine onto the model's channel,
// plus the async result of validating a freshly entered key.
type (
	deltaMsg    string
	doneMsg     struct{ err error }
	keyCheckMsg struct {
		provider string
		key      string
		api      llm.LLMApi
		err      error
	}
)

// chanStreamer forwards output deltas onto a channel the Bubble Tea program
// drains, so streamed tokens become UI updates.
type chanStreamer struct {
	streaming.NoStreamer
	sub chan tea.Msg
}

func (c chanStreamer) Stream(m *streaming.AgentAsyncMessage) {
	if m == nil || m.Type != streaming.OutputMessageType {
		return
	}
	if s, ok := m.Output.(string); ok && s != "" {
		c.sub <- deltaMsg(s)
	}
}

type styles struct {
	user, ai, model, status, help, errLine, cmd, hdr lipgloss.Style
}

func newStyles() styles {
	return styles{
		user:    lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true),
		ai:      lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true),
		model:   lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true), // yellow
		status:  lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		help:    lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		errLine: lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
		cmd:     lipgloss.NewStyle().Foreground(lipgloss.Color("14")),
		hdr:     lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true),
	}
}

type model struct {
	api      llm.LLMApi
	provider string
	model    string
	system   string

	vp     viewport.Model
	ta     textarea.Model
	styles styles

	// Masked key-entry mode (for /key).
	keyInput    textinput.Model
	keyMode     bool
	keyProvider string

	history    []types.ConversationMessage // newest-first
	transcript string                      // finalized, rendered turns
	cur        string                      // assistant text for the in-flight turn
	curUser    string                      // the user message being answered
	streaming  bool

	sub           chan tea.Msg
	width, height int

	// Shell-style input history (Up/Down recall).
	inputHistory []string
	histIdx      int    // -1 = composing a new line
	draft        string // unsent text stashed while browsing history
}

func initialModel(api llm.LLMApi, provider, mdl, system string) model {
	ta := textarea.New()
	ta.Placeholder = "Send a message..."
	ta.Prompt = "│ "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.Focus()

	ki := textinput.New()
	ki.EchoMode = textinput.EchoPassword
	ki.EchoCharacter = '•'
	ki.Prompt = "key › "
	ki.Placeholder = "paste API key, Enter to set, Esc to cancel"

	st := newStyles()
	m := model{
		api:        api,
		provider:   provider,
		model:      mdl,
		system:     system,
		ta:         ta,
		keyInput:   ki,
		vp:         viewport.New(80, 20),
		styles:     st,
		histIdx:    -1,
		sub:        make(chan tea.Msg, 256),
		transcript: st.help.Render("Ask anything. Enter sends · Ctrl+C quits · /provider /model /key to manage · /help."),
	}
	return m
}

func (m model) Init() tea.Cmd { return textarea.Blink }

// waitForActivity yields the next streaming event from the channel.
func waitForActivity(sub chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-sub }
}

func (m *model) aiPrefix() string { return m.styles.ai.Render("llm") + "  " }

// setInput replaces the textarea contents and parks the cursor at the end.
func (m *model) setInput(s string) {
	m.ta.SetValue(s)
	m.ta.CursorEnd()
}

// historyPrev recalls an older input (Up). Returns false if there is nothing to recall.
func (m *model) historyPrev() bool {
	if len(m.inputHistory) == 0 {
		return false
	}
	if m.histIdx == -1 {
		m.draft = m.ta.Value()
		m.histIdx = len(m.inputHistory) - 1
	} else if m.histIdx > 0 {
		m.histIdx--
	}
	m.setInput(m.inputHistory[m.histIdx])
	return true
}

// historyNext moves toward newer inputs (Down), restoring the unsent draft past the end.
func (m *model) historyNext() bool {
	if m.histIdx == -1 {
		return false
	}
	if m.histIdx < len(m.inputHistory)-1 {
		m.histIdx++
		m.setInput(m.inputHistory[m.histIdx])
	} else {
		m.histIdx = -1
		m.setInput(m.draft)
	}
	return true
}

// recordHistory appends a submitted input, skipping an immediate duplicate.
func (m *model) recordHistory(text string) {
	if n := len(m.inputHistory); n == 0 || m.inputHistory[n-1] != text {
		m.inputHistory = append(m.inputHistory, text)
	}
	m.histIdx = -1
	m.draft = ""
}

// sysLine appends a dim informational line to the transcript.
func (m *model) sysLine(s string) {
	m.transcript += "\n\n" + m.styles.help.Render("• "+s)
}

// showHelp appends a formatted panel of commands, key bindings, and current state.
func (m *model) showHelp() {
	s := m.styles
	name := s.cmd.Width(22)
	row := func(k, desc string) string { return "  " + name.Render(k) + s.help.Render(desc) }

	block := strings.Join([]string{
		s.hdr.Render("Commands"),
		row("/provider <name>", "switch provider — anthropic · openai · google"),
		row("/model <name>", "switch model within the current provider"),
		row("/key [provider]", "set/replace a key (masked, validated, saved to .env)"),
		row("/help", "show this help"),
		row("exit", "quit the chat"),
		"",
		s.hdr.Render("Keys"),
		row("Enter", "send message"),
		row("Up / Down", "recall input history"),
		row("PgUp / PgDn / wheel", "scroll the history"),
		row("Ctrl+C", "quit"),
		"",
		s.help.Render("now: ") + s.status.Render(m.provider+" · ") + s.model.Render(m.model),
	}, "\n")

	m.transcript += "\n\n" + block
}

// switchProvider swaps the active provider if its API key is already available.
// The full-screen TUI cannot prompt for a hidden key, so a missing key is
// reported rather than prompted.
func (m *model) switchProvider(name string) {
	envVar := chatkit.EnvVarFor(name)
	if envVar == "" {
		m.sysLine("unknown provider: " + name + "   (want anthropic, openai, google)")
		return
	}
	if name == m.provider {
		m.sysLine("already using " + name)
		return
	}
	if os.Getenv(envVar) == "" {
		// No key yet: drop straight into masked key entry for this provider.
		m.sysLine(envVar + " is not set.")
		_ = m.enterKeyMode(name)
		return
	}
	api, err := chatkit.NewProvider(name)
	if err != nil || api == nil {
		m.sysLine("could not switch to " + name)
		return
	}
	m.api = api
	m.provider = name
	m.model = chatkit.ModelFor(name)
	m.sysLine("provider set to " + name + "   (" + m.model + ")")
}

// enterKeyMode switches the bottom input to a masked field for entering the
// given provider's API key.
func (m *model) enterKeyMode(provider string) tea.Cmd {
	m.keyMode = true
	m.keyProvider = provider
	m.keyInput.Reset()
	m.ta.Blur()
	m.sysLine("enter the " + chatkit.EnvVarFor(provider) + " for " + provider + " (hidden). Enter to set, Esc to cancel.")
	return m.keyInput.Focus()
}

func (m *model) exitKeyMode() {
	m.keyMode = false
	m.keyInput.Blur()
	m.keyInput.Reset()
	m.ta.Focus()
}

// beginKeyCheck sets a provider's key for the session and returns a command
// that validates it off the UI thread. The key is only saved to .env and made
// active once validation succeeds (handled in Update on keyCheckMsg).
func (m *model) beginKeyCheck(provider, key string) tea.Cmd {
	envVar := chatkit.EnvVarFor(provider)
	_ = os.Setenv(envVar, key)
	config.Reload()

	api, err := chatkit.NewProvider(provider)
	if err != nil || api == nil {
		_ = os.Unsetenv(envVar)
		config.Reload()
		m.sysLine("could not create " + provider + " client.")
		return nil
	}

	m.sysLine("checking " + provider + " key…")
	return func() tea.Msg {
		return keyCheckMsg{provider: provider, key: key, api: api, err: chatkit.ValidateKey(api)}
	}
}

// saveAndActivate persists a validated key and makes its provider active.
func (m *model) saveAndActivate(msg keyCheckMsg, note string) {
	saved := "saved to .env"
	if err := chatkit.SaveDotEnv(dotEnvPath, chatkit.EnvVarFor(msg.provider), msg.key); err != nil {
		saved = "could not save to .env: " + err.Error()
	}
	m.api = msg.api
	m.provider = msg.provider
	m.model = chatkit.ModelFor(msg.provider)
	m.sysLine(note + " provider set to " + msg.provider + " (" + m.model + "), " + saved)
}

// handleCommand runs a slash command and reports whether the input was one.
func (m *model) handleCommand(text string) bool {
	if !strings.HasPrefix(text, "/") {
		return false
	}
	fields := strings.Fields(text)
	switch fields[0] {
	case "/model":
		if len(fields) < 2 {
			m.sysLine("current model: " + m.model + "   (use: /model <name>)")
			break
		}
		name := fields[1]
		config.SetModel(m.provider, name)
		m.model = name
		m.sysLine("model set to " + name)
	case "/provider":
		if len(fields) < 2 {
			m.sysLine("current provider: " + m.provider + "   (use: /provider anthropic|openai|google)")
			break
		}
		m.switchProvider(fields[1])
	case "/key":
		target := m.provider
		if len(fields) >= 2 {
			target = fields[1]
		}
		if chatkit.EnvVarFor(target) == "" {
			m.sysLine("unknown provider: " + target + "   (want anthropic, openai, google)")
			break
		}
		_ = m.enterKeyMode(target)
	case "/help":
		m.showHelp()
	default:
		m.sysLine("unknown command: " + fields[0] + "   (try /help)")
	}
	return true
}

// refresh rebuilds the viewport content (finalized transcript plus the
// in-flight assistant text) and wraps it to the viewport width.
func (m *model) refresh() {
	content := m.transcript
	if m.streaming {
		content += "\n\n" + m.aiPrefix() + m.cur
	}
	if m.vp.Width > 0 {
		content = lipgloss.NewStyle().Width(m.vp.Width).Render(content)
	}
	m.vp.SetContent(content)
}

func (m *model) startTurn() tea.Cmd {
	userMsg := strings.TrimSpace(m.ta.Value())
	m.ta.Reset()

	m.transcript += "\n\n" + m.styles.user.Render("you") + "  " + userMsg
	m.curUser = userMsg
	m.cur = ""
	m.streaming = true
	m.refresh()
	m.vp.GotoBottom()

	histSnapshot := append([]types.ConversationMessage(nil), m.history...)
	api, sub, system := m.api, m.sub, m.system

	go func() {
		req := llm.NewAIRequest("tui", system, nil, nil, nil)
		req.AddConversationHistory(histSnapshot)
		req.AddUserMessage(userMsg)
		_, err := api.Stream(context.Background(), req, chanStreamer{sub: sub})
		sub <- doneMsg{err: err}
	}()

	return waitForActivity(m.sub)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Masked key entry takes over the input while active.
		if m.keyMode {
			switch msg.Type {
			case tea.KeyCtrlC:
				return m, tea.Quit
			case tea.KeyEsc:
				m.exitKeyMode()
				m.sysLine("key entry cancelled.")
				m.refresh()
				m.vp.GotoBottom()
				return m, nil
			case tea.KeyEnter:
				key := strings.TrimSpace(m.keyInput.Value())
				provider := m.keyProvider
				m.exitKeyMode()
				var cmd tea.Cmd
				if key == "" {
					m.sysLine("no key entered.")
				} else {
					cmd = m.beginKeyCheck(provider, key)
				}
				m.refresh()
				m.vp.GotoBottom()
				return m, cmd
			}
			var cmd tea.Cmd
			m.keyInput, cmd = m.keyInput.Update(msg)
			return m, cmd
		}

		// Intercept control keys before the textarea sees them so Enter sends
		// (rather than inserting a newline) and Ctrl+C quits.
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyUp:
			// Recall older input only at the top line; otherwise move the cursor.
			if m.ta.Line() == 0 && m.historyPrev() {
				return m, nil
			}
		case tea.KeyDown:
			// Recall newer input only at the bottom line; otherwise move the cursor.
			if m.ta.Line() == m.ta.LineCount()-1 && m.historyNext() {
				return m, nil
			}
		case tea.KeyEnter:
			if m.streaming {
				return m, nil // ignore while a reply is streaming
			}
			text := strings.TrimSpace(m.ta.Value())
			if text == "" {
				return m, nil
			}
			if text == "exit" || text == "quit" {
				return m, tea.Quit
			}
			m.recordHistory(text)
			if m.handleCommand(text) {
				m.ta.Reset()
				m.refresh()
				m.vp.GotoBottom()
				if m.keyMode {
					return m, textinput.Blink
				}
				return m, nil
			}
			return m, m.startTurn()
		}

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		const taHeight, statusHeight = 3, 1
		m.ta.SetWidth(msg.Width)
		m.ta.SetHeight(taHeight)
		m.keyInput.Width = msg.Width - 10
		m.vp.Width = msg.Width
		m.vp.Height = msg.Height - taHeight - statusHeight - 1
		if m.vp.Height < 1 {
			m.vp.Height = 1
		}
		m.refresh()
		m.vp.GotoBottom()
		return m, nil

	case keyCheckMsg:
		if msg.err == nil {
			m.saveAndActivate(msg, "key valid —")
			m.refresh()
			m.vp.GotoBottom()
			return m, nil
		}
		// Verification failed: never save, never switch. Drop the session key we
		// set for the check, and restore any previously-saved good key from .env.
		_ = os.Unsetenv(chatkit.EnvVarFor(msg.provider))
		config.LoadDotEnv(dotEnvPath)
		if chatkit.IsAuthErr(msg.err) {
			m.sysLine("key rejected: " + chatkit.FriendlyErr(msg.err) + "  (not saved, provider unchanged)")
			cmd := m.enterKeyMode(msg.provider) // likely a typo; let them retry
			m.refresh()
			m.vp.GotoBottom()
			return m, tea.Batch(cmd, textinput.Blink)
		}
		if chatkit.IsTransientErr(msg.err) {
			m.sysLine(msg.provider + " is busy right now: " + chatkit.FriendlyErr(msg.err) + "  Key not saved; run /key " + msg.provider + " to try again.")
		} else {
			m.sysLine("could not verify key: " + chatkit.FriendlyErr(msg.err) + "  (not saved, provider unchanged)")
		}
		m.refresh()
		m.vp.GotoBottom()
		return m, nil

	case deltaMsg:
		m.cur += string(msg)
		m.refresh()
		m.vp.GotoBottom()
		return m, waitForActivity(m.sub)

	case doneMsg:
		full := m.cur
		m.transcript += "\n\n" + m.aiPrefix() + full
		if msg.err != nil {
			m.transcript += "\n" + m.styles.errLine.Render("[error] "+chatkit.FriendlyErr(msg.err))
		}
		m.history = append([]types.ConversationMessage{
			{UserMessage: m.curUser, AssistantMessage: full},
		}, m.history...)
		m.streaming = false
		m.cur = ""
		m.curUser = ""
		m.refresh()
		m.vp.GotoBottom()
		return m, nil
	}

	// Typing and scrolling: forward to the active input and the history viewport.
	if m.keyMode {
		var cmd tea.Cmd
		m.keyInput, cmd = m.keyInput.Update(msg)
		return m, cmd
	}
	var taCmd, vpCmd tea.Cmd
	m.ta, taCmd = m.ta.Update(msg)
	m.vp, vpCmd = m.vp.Update(msg)
	return m, tea.Batch(taCmd, vpCmd)
}

func (m model) statusBar() string {
	left := m.styles.status.Render(" " + m.provider + "  ")
	mdl := m.styles.model.Render(m.model)
	state := " · enter send · ctrl+c quit"
	if m.streaming {
		state = " · streaming…"
	}
	return left + mdl + m.styles.help.Render(state)
}

func (m model) View() string {
	input := m.ta.View()
	if m.keyMode {
		// Pad the single-line masked field to the textarea's height so the
		// layout does not jump.
		input = lipgloss.NewStyle().Height(3).Render(m.keyInput.View())
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.vp.View(),
		input,
		m.statusBar(),
	)
}

func main() {
	provider := flag.String("provider", "anthropic", "anthropic | openai | google")
	system := flag.String("system", "You are a concise, helpful assistant.", "system prompt")
	flag.Parse()

	// Silence library logs; the TUI owns the screen.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Resolve and validate the key before taking over the screen.
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	api := chatkit.Connect(*provider, dotEnvPath, in)
	mdl := chatkit.ModelFor(*provider)

	p := tea.NewProgram(
		initialModel(api, *provider, mdl, *system),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(), // enable mouse wheel to scroll the history
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tui error:", err)
		os.Exit(1)
	}
}
