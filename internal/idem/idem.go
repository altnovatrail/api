// Package idem is what tells one request from another under one idempotency key
// (R17, R35): the key's form and the request's fingerprint.
//
// A fingerprint is of the request as protobuf writes it deterministically, with
// its key left out, so that a repeat of one request has the fingerprint of its
// first. Most are SHA-256. One of a request that carries a PIN or an age is
// argon2id with a salt of its own: a PIN has 10⁶ values and an age 18, so a fast
// hash of either would give it back, and the age is kept nowhere (privacy 3.3
// spec).
package idem

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/altnovatrail/api/internal/argon"
)

// MaxKey is the longest key, in bytes, that a record keeps.
const MaxKey = 128

// keyField is the name every request with a key gives it in the contract.
const keyField = "idempotency_key"

// fast is the mark of a fingerprint by SHA-256.
const fast = "sha256:"

// ValidKey says whether key may be recorded: present, no longer than MaxKey,
// and with no NUL, which the database's text cannot hold.
func ValidKey(key string) bool {
	return key != "" && len(key) <= MaxKey && !strings.ContainsRune(key, 0)
}

// Fingerprint is the fingerprint of req: argon2id when slow, SHA-256 otherwise.
func Fingerprint(req proto.Message, slow bool) string {
	written := bytesOf(req)
	if slow {
		return argon.Hash(written)
	}
	sum := sha256.Sum256(written)
	return fast + hex.EncodeToString(sum[:])
}

// Same says whether req is the request fingerprint was taken of.
func Same(req proto.Message, fingerprint string) bool {
	written := bytesOf(req)
	if argon.Is(fingerprint) {
		return argon.Matches(written, fingerprint)
	}
	sum := sha256.Sum256(written)
	return fingerprint == fast+hex.EncodeToString(sum[:])
}

// bytesOf is req without its key, as protobuf writes it deterministically. A
// request of the contract always marshals.
func bytesOf(req proto.Message) []byte {
	keyless := proto.Clone(req)
	m := keyless.ProtoReflect()
	if field := m.Descriptor().Fields().ByName(keyField); field != nil {
		m.Clear(field)
	}
	written, _ := proto.MarshalOptions{Deterministic: true}.Marshal(keyless)
	return written
}
