// Package tray implements StatusNotifierItem and the D-Bus menu protocol without GTK.
package tray

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
	"wireguard-linux-gui/internal/desktopbus"
)

const (
	itemInterface                 = "org.kde.StatusNotifierItem"
	menuInterface                 = "com.canonical.dbusmenu"
	watcher                       = "org.kde.StatusNotifierWatcher"
	itemPath      dbus.ObjectPath = "/StatusNotifierItem"
	menuPath      dbus.ObjectPath = "/MenuBar"
)

type pixmap struct {
	Width, Height int32
	Data          []byte
}
type tooltip struct {
	Icon               string
	Pixmaps            []pixmap
	Title, Description string
}

type Item struct {
	Key       string
	Label     string
	Enabled   bool
	Visible   bool
	Checkable bool
	Checked   bool
	Separator bool
	Activate  func()
}

type layout struct {
	ID         int32
	Properties map[string]dbus.Variant
	Children   []dbus.Variant
}

type Tray struct {
	conn       *dbus.Conn
	properties *prop.Properties
	mu         sync.RWMutex
	items      map[int32]Item
	order      []int32
	ids        map[string]int32
	nextID     int32
	revision   uint32
	available  atomic.Bool
	closed     atomic.Bool
	signals    chan *dbus.Signal
	dispatch   func(func())
	changed    func(bool)
	activate   func()
}

// New exports a named icon and its search directory so the panel can recolor
// symbolic SVGs using its own theme, independently of the application theme.
func New(address, iconName, iconThemePath string, dispatch func(func()), activate func(), changed func(bool)) (*Tray, error) {
	if address == "" {
		return nil, fmt.Errorf("desktop session bus unavailable")
	}
	conn, err := desktopbus.Connect(address)
	if err != nil {
		return nil, err
	}
	t := &Tray{conn: conn, dispatch: dispatch, changed: changed, activate: activate, signals: make(chan *dbus.Signal, 16)}
	fail := func(err error) (*Tray, error) { conn.Close(); return nil, err }
	p := func(v interface{}) *prop.Prop { return &prop.Prop{Value: v, Emit: prop.EmitFalse} }
	t.properties, err = prop.Export(conn, itemPath, prop.Map{itemInterface: {
		"Category": p("ApplicationStatus"), "Id": p("wireguard-linux-gui"), "Title": p("WireGuard"),
		"Status": p("Active"), "WindowId": p(uint32(0)), "IconName": p(iconName),
		"IconThemePath": p(iconThemePath), "IconPixmap": p([]pixmap{}), "OverlayIconPixmap": p([]pixmap{}), "AttentionIconPixmap": p([]pixmap{}), "AttentionMovieName": p(""), "ToolTip": p(tooltip{Title: "WireGuard", Pixmaps: []pixmap{}}), "OverlayIconName": p(""), "AttentionIconName": p(""),
		"ItemIsMenu": p(false), "Menu": p(menuPath),
	}})
	if err != nil {
		return fail(err)
	}
	if err = conn.Export(&notifier{t}, itemPath, itemInterface); err != nil {
		return fail(err)
	}
	if err = conn.Export(&menu{t}, menuPath, menuInterface); err != nil {
		return fail(err)
	}
	menuProperties, err := prop.Export(conn, menuPath, prop.Map{menuInterface: {"Version": p(uint32(3)), "TextDirection": p("ltr"), "Status": p("normal"), "IconThemePath": p([]string{})}})
	if err != nil {
		return fail(err)
	}
	for path, iface := range map[dbus.ObjectPath]string{itemPath: itemInterface, menuPath: menuInterface} {
		var methods []introspect.Method
		if path == itemPath {
			methods = introspect.Methods(&notifier{t})
		} else {
			methods = introspect.Methods(&menu{t})
		}
		info := introspect.Interface{Name: iface, Methods: methods}
		if path == itemPath {
			info.Properties = t.properties.Introspection(iface)
			for _, name := range []string{"NewIcon", "NewTitle", "NewToolTip", "NewAttentionIcon", "NewOverlayIcon"} {
				info.Signals = append(info.Signals, introspect.Signal{Name: name})
			}
			info.Signals = append(info.Signals, introspect.Signal{Name: "NewStatus", Args: []introspect.Arg{{Name: "status", Type: "s"}}})
		} else {
			info.Properties = menuProperties.Introspection(iface)
			info.Signals = []introspect.Signal{{Name: "LayoutUpdated", Args: []introspect.Arg{{Name: "revision", Type: "u"}, {Name: "parent", Type: "i"}}}}
		}
		node := &introspect.Node{Interfaces: []introspect.Interface{info, prop.IntrospectData}}
		if err = conn.Export(introspect.NewIntrospectable(node), path, "org.freedesktop.DBus.Introspectable"); err != nil {
			return fail(err)
		}
	}
	conn.Signal(t.signals)
	if err = conn.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, watcher)); err != nil {
		return fail(err)
	}
	if err = conn.AddMatchSignal(dbus.WithMatchSender(watcher), dbus.WithMatchInterface(watcher)); err != nil {
		return fail(err)
	}
	t.register()
	go func() {
		for sig := range t.signals {
			if t.closed.Load() {
				return
			}
			if sig.Name == "org.freedesktop.DBus.NameOwnerChanged" {
				t.setAvailable(false)
				if len(sig.Body) == 3 && sig.Body[2] != "" {
					t.register()
				}
			} else if sig.Name == watcher+".StatusNotifierHostRegistered" {
				t.register()
			} else if sig.Name == watcher+".StatusNotifierHostUnregistered" {
				t.setAvailable(false)
			}
		}
		t.setAvailable(false)
	}()
	return t, nil
}
func (t *Tray) register() {
	obj := t.conn.Object(watcher, "/StatusNotifierWatcher")
	err := obj.Call(watcher+".RegisterStatusNotifierItem", 0, string(itemPath)).Err
	available := err == nil
	if available {
		if v, e := obj.GetProperty(watcher + ".IsStatusNotifierHostRegistered"); e == nil {
			available, _ = v.Value().(bool)
		}
	}
	t.setAvailable(available)
}
func (t *Tray) setAvailable(v bool) {
	if t.available.Swap(v) != v && !t.closed.Load() && t.changed != nil {
		t.dispatch(func() {
			if !t.closed.Load() {
				t.changed(v)
			}
		})
	}
}
func (t *Tray) Available() bool { return t != nil && t.available.Load() }
func (t *Tray) Close() {
	if t == nil || t.closed.Swap(true) {
		return
	}
	t.conn.Close()
}
func (t *Tray) Update(icon, title string, items []Item) {
	if t == nil || t.closed.Load() {
		return
	}
	t.mu.Lock()
	if t.ids == nil {
		t.ids = map[string]int32{}
	}
	t.items = map[int32]Item{}
	t.order = nil
	for i, item := range items {
		key := item.Key
		if key == "" {
			key = item.Label
			if key == "" {
				key = fmt.Sprintf("separator:%d", i)
			}
		}
		id, ok := t.ids[key]
		if !ok {
			t.nextID++
			id = t.nextID
			t.ids[key] = id
		}
		t.items[id] = item
		t.order = append(t.order, id)
	}
	t.revision++
	revision := t.revision
	t.mu.Unlock()
	t.properties.SetMust(itemInterface, "IconName", icon)
	t.properties.SetMust(itemInterface, "Title", title)
	t.properties.SetMust(itemInterface, "ToolTip", tooltip{Title: title, Pixmaps: []pixmap{}})
	t.conn.Emit(itemPath, itemInterface+".NewIcon")
	t.conn.Emit(itemPath, itemInterface+".NewTitle")
	t.conn.Emit(itemPath, itemInterface+".NewToolTip")
	t.conn.Emit(menuPath, menuInterface+".LayoutUpdated", revision, int32(0))
}
func properties(item Item) map[string]dbus.Variant {
	p := map[string]dbus.Variant{"label": dbus.MakeVariant(item.Label), "enabled": dbus.MakeVariant(item.Enabled), "visible": dbus.MakeVariant(item.Visible)}
	if item.Separator {
		p["type"] = dbus.MakeVariant("separator")
	}
	if item.Checkable {
		p["toggle-type"] = dbus.MakeVariant("checkmark")
		state := int32(0)
		if item.Checked {
			state = 1
		}
		p["toggle-state"] = dbus.MakeVariant(state)
	}
	return p
}

type notifier struct{ t *Tray }

func (n *notifier) Activate(x, y int32) *dbus.Error                    { n.t.dispatch(n.t.activate); return nil }
func (n *notifier) SecondaryActivate(x, y int32) *dbus.Error           { return n.Activate(x, y) }
func (n *notifier) ContextMenu(x, y int32) *dbus.Error                 { return nil }
func (n *notifier) Scroll(delta int32, orientation string) *dbus.Error { return nil }

type menu struct{ t *Tray }

func (m *menu) GetLayout(parentID, recursionDepth int32, propertyNames []string) (uint32, layout, *dbus.Error) {
	m.t.mu.RLock()
	defer m.t.mu.RUnlock()
	root := layout{ID: parentID, Properties: map[string]dbus.Variant{}, Children: []dbus.Variant{}}
	if item, ok := m.t.items[parentID]; ok {
		root.Properties = properties(item)
	} else if parentID == 0 {
		root.Properties["children-display"] = dbus.MakeVariant("submenu")
		if recursionDepth != 0 {
			for _, id := range m.t.order {
				root.Children = append(root.Children, dbus.MakeVariant(layout{id, properties(m.t.items[id]), []dbus.Variant{}}))
			}
		}
	}
	return m.t.revision, root, nil
}

type propertyGroup struct {
	ID         int32
	Properties map[string]dbus.Variant
}

func (m *menu) GetGroupProperties(ids []int32, names []string) ([]propertyGroup, *dbus.Error) {
	m.t.mu.RLock()
	defer m.t.mu.RUnlock()
	result := []propertyGroup{}
	if len(ids) == 0 {
		for _, id := range m.t.order {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		if item, ok := m.t.items[id]; ok {
			result = append(result, propertyGroup{id, properties(item)})
		}
	}
	return result, nil
}
func (m *menu) GetProperty(id int32, name string) (dbus.Variant, *dbus.Error) {
	m.t.mu.RLock()
	defer m.t.mu.RUnlock()
	if item, ok := m.t.items[id]; ok {
		if v, ok := properties(item)[name]; ok {
			return v, nil
		}
	}
	return dbus.MakeVariant(""), dbus.NewError("com.canonical.dbusmenu.Error.InvalidProperty", nil)
}
func (m *menu) Event(id int32, event string, data dbus.Variant, timestamp uint32) *dbus.Error {
	m.t.mu.RLock()
	var f func()
	if item, ok := m.t.items[id]; ok {
		if item.Enabled && item.Visible && event == "clicked" {
			f = item.Activate
		}
	}
	m.t.mu.RUnlock()
	if f != nil {
		m.t.dispatch(f)
	}
	return nil
}

type event struct {
	ID        int32
	Event     string
	Data      dbus.Variant
	Timestamp uint32
}

func (m *menu) EventGroup(events []event) ([]int32, *dbus.Error) {
	for _, e := range events {
		m.Event(e.ID, e.Event, e.Data, e.Timestamp)
	}
	return []int32{}, nil
}
func (m *menu) AboutToShow(id int32) (bool, *dbus.Error) { return false, nil }
func (m *menu) AboutToShowGroup(ids []int32) ([]int32, []int32, *dbus.Error) {
	return []int32{}, []int32{}, nil
}
