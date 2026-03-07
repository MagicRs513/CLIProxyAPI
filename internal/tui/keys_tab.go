package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// keysTabModel displays and manages API keys.
type keysTabModel struct {
	client   *Client
	viewport viewport.Model
	keys     []string
	settings map[string]apiKeyPolicy
	gemini   []map[string]any
	claude   []map[string]any
	codex    []map[string]any
	vertex   []map[string]any
	openai   []map[string]any
	err      error
	width    int
	height   int
	ready    bool
	cursor   int
	confirm  int // -1 = no deletion pending
	status   string

	// Editing / Adding
	editing   bool
	adding    bool
	editIdx   int
	editInput textinput.Model
}

type apiKeyPolicy struct {
	ExpiresAt  string
	TokenLimit int64
}

type keysDataMsg struct {
	apiKeys []string
	policy  []map[string]any
	gemini  []map[string]any
	claude  []map[string]any
	codex   []map[string]any
	vertex  []map[string]any
	openai  []map[string]any
	err     error
}

type keyActionMsg struct {
	action string
	err    error
}

func newKeysTabModel(client *Client) keysTabModel {
	ti := textinput.New()
	ti.CharLimit = 512
	ti.Prompt = "  Key: "
	return keysTabModel{
		client:    client,
		confirm:   -1,
		editInput: ti,
	}
}

func (m keysTabModel) Init() tea.Cmd {
	return m.fetchKeys
}

func (m keysTabModel) fetchKeys() tea.Msg {
	result := keysDataMsg{}
	apiKeys, err := m.client.GetAPIKeys()
	if err != nil {
		result.err = err
		return result
	}
	result.apiKeys = apiKeys
	result.policy, _ = m.client.GetAPIKeySettings()
	result.gemini, _ = m.client.GetGeminiKeys()
	result.claude, _ = m.client.GetClaudeKeys()
	result.codex, _ = m.client.GetCodexKeys()
	result.vertex, _ = m.client.GetVertexKeys()
	result.openai, _ = m.client.GetOpenAICompat()
	return result
}

func (m keysTabModel) Update(msg tea.Msg) (keysTabModel, tea.Cmd) {
	switch msg := msg.(type) {
	case localeChangedMsg:
		m.viewport.SetContent(m.renderContent())
		return m, nil
	case keysDataMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.err = nil
			m.keys = msg.apiKeys
			m.settings = toPolicyMap(msg.policy)
			m.gemini = msg.gemini
			m.claude = msg.claude
			m.codex = msg.codex
			m.vertex = msg.vertex
			m.openai = msg.openai
			if m.cursor >= len(m.keys) {
				m.cursor = max(0, len(m.keys)-1)
			}
		}
		m.viewport.SetContent(m.renderContent())
		return m, nil

	case keyActionMsg:
		if msg.err != nil {
			m.status = errorStyle.Render("✗ " + msg.err.Error())
		} else {
			m.status = successStyle.Render("✓ " + msg.action)
		}
		m.confirm = -1
		m.viewport.SetContent(m.renderContent())
		return m, m.fetchKeys

	case tea.KeyMsg:
		// ---- Editing / Adding mode ----
		if m.editing || m.adding {
			switch msg.String() {
			case "enter":
				raw := strings.TrimSpace(m.editInput.Value())
				if raw == "" {
					m.editing = false
					m.adding = false
					m.editInput.Blur()
					m.viewport.SetContent(m.renderContent())
					return m, nil
				}
				value, policy, hasPolicy, parseErr := parseKeyInput(raw)
				if parseErr != nil {
					m.status = errorStyle.Render("✗ " + parseErr.Error())
					m.viewport.SetContent(m.renderContent())
					return m, nil
				}
				isAdding := m.adding
				editIdx := m.editIdx
				oldKey := ""
				if editIdx >= 0 && editIdx < len(m.keys) {
					oldKey = m.keys[editIdx]
				}
				m.editing = false
				m.adding = false
				m.editInput.Blur()
				if isAdding {
					return m, func() tea.Msg {
						err := m.client.AddAPIKey(value)
						if err != nil {
							return keyActionMsg{err: err}
						}
						if hasPolicy {
							if err = reconcileAPIKeyPolicy(m.client, "", value, &policy); err != nil {
								return keyActionMsg{err: err}
							}
						}
						return keyActionMsg{action: T("key_added")}
					}
				}
				return m, func() tea.Msg {
					err := m.client.EditAPIKey(editIdx, value)
					if err != nil {
						return keyActionMsg{err: err}
					}
					if hasPolicy || oldKey != value {
						if err = reconcileAPIKeyPolicy(m.client, oldKey, value, policyOrNil(hasPolicy, policy)); err != nil {
							return keyActionMsg{err: err}
						}
					}
					return keyActionMsg{action: T("key_updated")}
				}
			case "esc":
				m.editing = false
				m.adding = false
				m.editInput.Blur()
				m.viewport.SetContent(m.renderContent())
				return m, nil
			default:
				var cmd tea.Cmd
				m.editInput, cmd = m.editInput.Update(msg)
				m.viewport.SetContent(m.renderContent())
				return m, cmd
			}
		}

		// ---- Delete confirmation ----
		if m.confirm >= 0 {
			switch msg.String() {
			case "y", "Y":
				idx := m.confirm
				m.confirm = -1
				return m, func() tea.Msg {
					err := m.client.DeleteAPIKey(idx)
					if err != nil {
						return keyActionMsg{err: err}
					}
					return keyActionMsg{action: T("key_deleted")}
				}
			case "n", "N", "esc":
				m.confirm = -1
				m.viewport.SetContent(m.renderContent())
				return m, nil
			}
			return m, nil
		}

		// ---- Normal mode ----
		switch msg.String() {
		case "j", "down":
			if len(m.keys) > 0 {
				m.cursor = (m.cursor + 1) % len(m.keys)
				m.viewport.SetContent(m.renderContent())
			}
			return m, nil
		case "k", "up":
			if len(m.keys) > 0 {
				m.cursor = (m.cursor - 1 + len(m.keys)) % len(m.keys)
				m.viewport.SetContent(m.renderContent())
			}
			return m, nil
		case "a":
			// Add new key
			m.adding = true
			m.editing = false
			m.editInput.SetValue("")
			m.editInput.Prompt = T("new_key_prompt")
			m.editInput.Focus()
			m.viewport.SetContent(m.renderContent())
			return m, textinput.Blink
		case "e":
			// Edit selected key
			if m.cursor < len(m.keys) {
				m.editing = true
				m.adding = false
				m.editIdx = m.cursor
				m.editInput.SetValue(m.keys[m.cursor])
				m.editInput.Prompt = T("edit_key_prompt")
				m.editInput.Focus()
				m.viewport.SetContent(m.renderContent())
				return m, textinput.Blink
			}
			return m, nil
		case "d":
			// Delete selected key
			if m.cursor < len(m.keys) {
				m.confirm = m.cursor
				m.viewport.SetContent(m.renderContent())
			}
			return m, nil
		case "c":
			// Copy selected key to clipboard
			if m.cursor < len(m.keys) {
				key := m.keys[m.cursor]
				if err := clipboard.WriteAll(key); err != nil {
					m.status = errorStyle.Render(T("copy_failed") + ": " + err.Error())
				} else {
					m.status = successStyle.Render(T("copied"))
				}
				m.viewport.SetContent(m.renderContent())
			}
			return m, nil
		case "r":
			m.status = ""
			return m, m.fetchKeys
		default:
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *keysTabModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.editInput.Width = w - 16
	if !m.ready {
		m.viewport = viewport.New(w, h)
		m.viewport.SetContent(m.renderContent())
		m.ready = true
	} else {
		m.viewport.Width = w
		m.viewport.Height = h
	}
}

func (m keysTabModel) View() string {
	if !m.ready {
		return T("loading")
	}
	return m.viewport.View()
}

func (m keysTabModel) renderContent() string {
	var sb strings.Builder

	sb.WriteString(titleStyle.Render(T("keys_title")))
	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render(T("keys_help")))
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("─", m.width))
	sb.WriteString("\n")

	if m.err != nil {
		sb.WriteString(errorStyle.Render(T("error_prefix") + m.err.Error()))
		sb.WriteString("\n")
		return sb.String()
	}

	// ━━━ Access API Keys (interactive) ━━━
	sb.WriteString(tableHeaderStyle.Render(fmt.Sprintf("  %s (%d)", T("access_keys"), len(m.keys))))
	sb.WriteString("\n")

	if len(m.keys) == 0 {
		sb.WriteString(subtitleStyle.Render(T("no_keys")))
		sb.WriteString("\n")
	}

	for i, key := range m.keys {
		cursor := "  "
		rowStyle := lipgloss.NewStyle()
		if i == m.cursor {
			cursor = "▸ "
			rowStyle = lipgloss.NewStyle().Bold(true)
		}

		row := fmt.Sprintf("%s%d. %s", cursor, i+1, maskKey(key))
		if policy, ok := m.settings[key]; ok {
			parts := make([]string, 0, 2)
			if policy.ExpiresAt != "" {
				parts = append(parts, "exp: "+policy.ExpiresAt)
			}
			if policy.TokenLimit > 0 {
				parts = append(parts, "token-limit: "+strconv.FormatInt(policy.TokenLimit, 10))
			}
			if len(parts) > 0 {
				row += " [" + strings.Join(parts, ", ") + "]"
			}
		}
		sb.WriteString(rowStyle.Render(row))
		sb.WriteString("\n")

		// Delete confirmation
		if m.confirm == i {
			sb.WriteString(warningStyle.Render(fmt.Sprintf("    "+T("confirm_delete_key"), maskKey(key))))
			sb.WriteString("\n")
		}

		// Edit input
		if m.editing && m.editIdx == i {
			sb.WriteString(m.editInput.View())
			sb.WriteString("\n")
			sb.WriteString(helpStyle.Render(T("enter_save_esc")))
			sb.WriteString("\n")
		}
	}

	// Add input
	if m.adding {
		sb.WriteString("\n")
		sb.WriteString(m.editInput.View())
		sb.WriteString("\n")
		sb.WriteString(helpStyle.Render(T("enter_add")))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")

	// ━━━ Provider Keys (read-only display) ━━━
	renderProviderKeys(&sb, "Gemini API Keys", m.gemini)
	renderProviderKeys(&sb, "Claude API Keys", m.claude)
	renderProviderKeys(&sb, "Codex API Keys", m.codex)
	renderProviderKeys(&sb, "Vertex API Keys", m.vertex)

	if len(m.openai) > 0 {
		renderSection(&sb, "OpenAI Compatibility", len(m.openai))
		for i, entry := range m.openai {
			name := getString(entry, "name")
			baseURL := getString(entry, "base-url")
			prefix := getString(entry, "prefix")
			info := name
			if prefix != "" {
				info += " (prefix: " + prefix + ")"
			}
			if baseURL != "" {
				info += " → " + baseURL
			}
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, info))
		}
		sb.WriteString("\n")
	}

	if m.status != "" {
		sb.WriteString(m.status)
		sb.WriteString("\n")
	}

	return sb.String()
}

func renderSection(sb *strings.Builder, title string, count int) {
	header := fmt.Sprintf("%s (%d)", title, count)
	sb.WriteString(tableHeaderStyle.Render("  " + header))
	sb.WriteString("\n")
}

func renderProviderKeys(sb *strings.Builder, title string, keys []map[string]any) {
	if len(keys) == 0 {
		return
	}
	renderSection(sb, title, len(keys))
	for i, key := range keys {
		apiKey := getString(key, "api-key")
		prefix := getString(key, "prefix")
		baseURL := getString(key, "base-url")
		info := maskKey(apiKey)
		if prefix != "" {
			info += " (prefix: " + prefix + ")"
		}
		if baseURL != "" {
			info += " → " + baseURL
		}
		sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, info))
	}
	sb.WriteString("\n")
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return strings.Repeat("*", len(key))
	}
	return key[:4] + strings.Repeat("*", len(key)-8) + key[len(key)-4:]
}

func parseKeyInput(raw string) (string, apiKeyPolicy, bool, error) {
	parts := strings.Split(raw, "|")
	key := strings.TrimSpace(parts[0])
	if key == "" {
		return "", apiKeyPolicy{}, false, fmt.Errorf("API key is required")
	}
	if len(parts) == 1 {
		return key, apiKeyPolicy{}, false, nil
	}

	policy := apiKeyPolicy{}
	for i := 1; i < len(parts); i++ {
		segment := strings.TrimSpace(parts[i])
		if segment == "" {
			continue
		}
		kv := strings.SplitN(segment, "=", 2)
		if len(kv) != 2 {
			return "", apiKeyPolicy{}, false, fmt.Errorf("invalid policy segment: %s", segment)
		}
		name := strings.ToLower(strings.TrimSpace(kv[0]))
		value := strings.TrimSpace(kv[1])
		switch name {
		case "expires", "expire", "expires-at":
			policy.ExpiresAt = value
		case "tokens", "token-limit", "quota":
			v, err := strconv.ParseInt(value, 10, 64)
			if err != nil || v < 0 {
				return "", apiKeyPolicy{}, false, fmt.Errorf("invalid token limit")
			}
			policy.TokenLimit = v
		default:
			return "", apiKeyPolicy{}, false, fmt.Errorf("unknown policy field: %s", name)
		}
	}
	if policy.ExpiresAt == "" && policy.TokenLimit == 0 {
		return key, apiKeyPolicy{}, false, nil
	}
	return key, policy, true, nil
}

func policyOrNil(hasPolicy bool, policy apiKeyPolicy) *apiKeyPolicy {
	if !hasPolicy {
		return nil
	}
	copy := policy
	return &copy
}

func reconcileAPIKeyPolicy(client *Client, oldKey, newKey string, explicit *apiKeyPolicy) error {
	if client == nil {
		return nil
	}
	items, err := client.GetAPIKeySettings()
	if err != nil {
		return err
	}
	policies := toPolicyMap(items)
	if policies == nil {
		policies = map[string]apiKeyPolicy{}
	}

	oldKey = strings.TrimSpace(oldKey)
	newKey = strings.TrimSpace(newKey)
	if oldKey != "" && oldKey != newKey {
		if policy, ok := policies[oldKey]; ok && explicit == nil {
			policies[newKey] = policy
		}
		delete(policies, oldKey)
	}

	if explicit != nil {
		if explicit.ExpiresAt == "" && explicit.TokenLimit == 0 {
			delete(policies, newKey)
		} else {
			policies[newKey] = *explicit
		}
	}

	keys := make([]string, 0, len(policies))
	for key := range policies {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	payload := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		policy := policies[key]
		entry := map[string]any{"api-key": key}
		if policy.ExpiresAt != "" {
			entry["expires-at"] = policy.ExpiresAt
		}
		if policy.TokenLimit > 0 {
			entry["token-limit"] = policy.TokenLimit
		}
		payload = append(payload, entry)
	}

	return client.PutAPIKeySettings(payload)
}

func toPolicyMap(items []map[string]any) map[string]apiKeyPolicy {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]apiKeyPolicy, len(items))
	for _, item := range items {
		apiKey := getString(item, "api-key")
		if apiKey == "" {
			continue
		}
		policy := apiKeyPolicy{ExpiresAt: getString(item, "expires-at")}
		switch raw := item["token-limit"].(type) {
		case float64:
			policy.TokenLimit = int64(raw)
		case int64:
			policy.TokenLimit = raw
		case int:
			policy.TokenLimit = int64(raw)
		}
		out[apiKey] = policy
	}
	return out
}
