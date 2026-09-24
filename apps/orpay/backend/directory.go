package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/openreserve/node/jsonstore"
	"github.com/openreserve/node/types"
)

var usernameRe = regexp.MustCompile(`^[a-z0-9_]{3,20}$`)

// RegisterMessage is what the wallet signs to claim a username.
func RegisterMessage(name string, addr types.Address) string {
	return fmt.Sprintf("orpay/register/v1\n%s\n%s", name, addr)
}

// directory maps usernames to addresses. Each address may hold one
// username and names are first come, first served.
type directory struct {
	store  *jsonstore.Store[map[string]types.Address]
	byAddr map[types.Address]string // guarded by store.mu
}

func openDirectory(path string) (*directory, error) {
	st, err := jsonstore.Open(path, map[string]types.Address{})
	if err != nil {
		return nil, err
	}
	d := &directory{store: st, byAddr: map[types.Address]string{}}
	st.Read(func(users map[string]types.Address) {
		for name, addr := range users {
			d.byAddr[addr] = name
		}
	})
	return d, nil
}

func (d *directory) claim(name string, addr types.Address) error {
	return d.store.Update(func(users map[string]types.Address) error {
		if owner, ok := users[name]; ok {
			if owner == addr {
				return nil
			}
			return fmt.Errorf("@%s is taken", name)
		}
		if existing, ok := d.byAddr[addr]; ok {
			return fmt.Errorf("this wallet is already @%s", existing)
		}
		users[name] = addr
		d.byAddr[addr] = name
		return nil
	})
}

func (d *directory) byName(name string) (a types.Address, ok bool) {
	d.store.Read(func(users map[string]types.Address) { a, ok = users[name] })
	return
}

func (d *directory) byAddress(addr types.Address) (n string, ok bool) {
	d.store.Read(func(map[string]types.Address) { n, ok = d.byAddr[addr] })
	return
}

// resolveRef turns "@name", "name" or an address into an address.
func (d *directory) resolveRef(ref string) (types.Address, error) {
	ref = strings.TrimSpace(ref)
	if a := types.Address(ref); a.Validate() == nil {
		return a, nil
	}
	name := strings.ToLower(strings.TrimPrefix(ref, "@"))
	if a, ok := d.byName(name); ok {
		return a, nil
	}
	return "", fmt.Errorf("no user @%s", name)
}

func (s *server) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string        `json:"username"`
		Address  types.Address `json:"address"`
		Sig      string        `json:"sig"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	req.Username = strings.ToLower(req.Username)
	if !usernameRe.MatchString(req.Username) {
		writeErr(w, http.StatusBadRequest, errors.New("username must be 3-20 characters: a-z, 0-9, _"))
		return
	}
	pub, err := req.Address.PubKey()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sig, err := hex.DecodeString(req.Sig)
	if err != nil || !ed25519.Verify(pub, []byte(RegisterMessage(req.Username, req.Address)), sig) {
		writeErr(w, http.StatusUnauthorized, errors.New("signature does not match address"))
		return
	}
	if err := s.dir.claim(req.Username, req.Address); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"username": req.Username, "address": req.Address})
}

func (s *server) resolve(w http.ResponseWriter, r *http.Request) {
	name := strings.ToLower(strings.TrimPrefix(r.PathValue("name"), "@"))
	addr, ok := s.dir.byName(name)
	if !ok {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no user @%s", name))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"username": name, "address": addr})
}

// lookup answers 200 with a null username for unknown addresses: having no
// username is normal, not an error.
func (s *server) lookup(w http.ResponseWriter, r *http.Request) {
	addr := types.Address(r.PathValue("addr"))
	var username any
	if name, ok := s.dir.byAddress(addr); ok {
		username = name
	}
	writeJSON(w, http.StatusOK, map[string]any{"username": username, "address": addr})
}
