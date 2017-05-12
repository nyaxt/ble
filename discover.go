package ble

import (
	"fmt"
	"log"
	"time"

	"github.com/godbus/dbus"
)

func (conn *Connection) addMatch(rule string) error {
	return conn.bus.BusObject().Call(
		"org.freedesktop.DBus.AddMatch",
		0,
		rule,
	).Err
}

func (conn *Connection) removeMatch(rule string) error {
	return conn.bus.BusObject().Call(
		"org.freedesktop.DBus.RemoveMatch",
		0,
		rule,
	).Err
}

// DiscoveryTimeoutError indicates that discovery has timed out.
type DiscoveryTimeoutError []string

func (e DiscoveryTimeoutError) Error() string {
	return fmt.Sprintf("discovery timeout %v", []string(e))
}

// Discover puts the adapter in discovery mode,
// waits for the specified timeout to discover one of the given UUIDs,
// and then stops discovery mode.
func (adapter *blob) Discover(timeout time.Duration, uuids ...string) error {
	conn := adapter.conn
	signals := make(chan *dbus.Signal)
	defer close(signals)
	conn.bus.Signal(signals)
	defer conn.bus.RemoveSignal(signals)
	rule := "type='signal',interface='org.freedesktop.DBus.ObjectManager',member='InterfacesAdded'"
	err := adapter.conn.addMatch(rule)
	if err != nil {
		return err
	}
	defer conn.removeMatch(rule) // nolint
	err = adapter.SetDiscoveryFilter(uuids...)
	if err != nil {
		return err
	}
	err = adapter.StartDiscovery()
	if err != nil {
		return err
	}
	defer adapter.StopDiscovery() // nolint
	var t <-chan time.Time
	if timeout != 0 {
		t = time.After(timeout)
	}
	return adapter.discoverLoop(uuids, signals, t)
}

func (adapter *blob) discoverLoop(uuids []string, signals <-chan *dbus.Signal, timeout <-chan time.Time) error {
	for {
		select {
		case s := <-signals:
			switch s.Name {
			case interfacesAdded:
				if adapter.discoveryComplete(s, uuids) {
					return nil
				}
			default:
				log.Printf("%s: unexpected signal %s", adapter.Name(), s.Name)
			}
		case <-timeout:
			return DiscoveryTimeoutError(uuids)
		}
	}
}

func (adapter *blob) discoveryComplete(s *dbus.Signal, uuids []string) bool {
	props := interfaceProperties(s)
	if props == nil {
		log.Printf("%s: skipping signal with no device interface", adapter.Name())
		return false
	}
	name := props["Name"].Value().(string)
	services := props["UUIDs"].Value().([]string)
	if uuidsInclude(services, uuids) {
		log.Printf("%s: discovered %s", adapter.Name(), name)
		return true
	}
	log.Printf("%s: skipping signal for device %s", adapter.Name(), name)
	return false
}

// If the InterfacesAdded signal contains deviceInterface,
// return the corresponding properties, otherwise nil.
// See http://dbus.freedesktop.org/doc/dbus-specification.html#standard-interfaces-objectmanager
func interfaceProperties(s *dbus.Signal) properties {
	var dict map[string]map[string]dbus.Variant
	err := dbus.Store(s.Body[1:2], &dict)
	if err != nil {
		log.Print(err)
		return nil
	}
	return dict[deviceInterface]
}

// Discover initiates discovery for a LE peripheral with the given UUIDs.
// It waits for at most the specified timeout, or indefinitely if timeout = 0.
func (conn *Connection) Discover(timeout time.Duration, uuids ...string) (Device, error) {
	adapter, err := conn.GetAdapter()
	if err != nil {
		return nil, err
	}
	err = adapter.Discover(timeout, uuids...)
	if err != nil {
		return nil, err
	}
	err = conn.Update()
	if err != nil {
		return nil, err
	}
	return conn.GetDevice(uuids...)
}
