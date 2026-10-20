// Package demorun publishes example key bindings to capable terminal hosts.
package demorun

import (
	"fmt"
	"os"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	booba "github.com/NimbleMarkets/go-booba"
	"github.com/NimbleMarkets/go-booba/actions"
	"github.com/NimbleMarkets/go-booba/actions/bubblehelp"
)

// Keys supplies the primary buttons and the expanded list of shortcuts.
type Keys struct {
	Primary []key.Binding
	More    []key.Binding
}

func (k Keys) ShortHelp() []key.Binding  { return k.Primary }
func (k Keys) FullHelp() [][]key.Binding { return [][]key.Binding{k.Primary, k.More} }

func Binding(shortcut, label string) key.Binding {
	return key.NewBinding(key.WithKeys(shortcut), key.WithHelp(shortcut, label))
}

// Run advertises ordinary key presses; the model remains their sole handler.
func Run(model tea.Model, context, summary string, keys Keys) error {
	wrapped, err := wrap(model, context, summary, keys, func(err error) { fmt.Fprintln(os.Stderr, err) })
	if err != nil {
		return err
	}
	return booba.Run(wrapped)
}

// Action metadata is optional; a bad description must not prevent startup.
func wrap(model tea.Model, context, summary string, keys Keys, onError func(error)) (*bubblehelp.Model, error) {
	return bubblehelp.NewModelWithFallback(model, func(tea.Model) bubblehelp.Description {
		return bubblehelp.Description{
			Context: context, Keys: keys,
			Status: actions.ProgramStatus{App: "ntmaps", Summary: summary},
		}
	}, bubblehelp.ModelOptions{OnError: onError})
}
