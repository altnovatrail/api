// Package idem is what tells one request from another under one idempotency key
// (R17, R35): the key's form and the request's fingerprint.
//
// A fingerprint is of the request as protobuf writes it deterministically, with
// its key left out, so that a repeat of one request has the fingerprint of its
// first, and its own key too: the learner's own key at a provider lives in the
// request's memory alone, and no hash of it is kept (R84 spec). Most are SHA-256. One of a request that carries a PIN or an age is
// argon2id with a salt of its own: a PIN has 10⁶ values and an age 18, so a fast
// hash of either would give it back, and the age is kept nowhere (privacy 3.3
// spec).
package idem

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/altnovatrail/api/internal/argon"
)

// MaxKey is the longest key, in bytes, that a record keeps.
const MaxKey = 128

// leftOut is the fields a fingerprint leaves out, by the names every request of
// the contract gives them: its idempotency key, and the learner's own key.
var leftOut = map[protoreflect.Name]bool{"idempotency_key": true, "own_key": true}

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

// bytesOf is req without the fields left out, as protobuf writes it
// deterministically. The copy is shallow: a request's work, a picture, is not
// copied to be written. A request of the contract always marshals.
func bytesOf(req proto.Message) []byte {
	m := req.ProtoReflect()
	kept := m.New()
	m.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if !leftOut[field.Name()] {
			kept.Set(field, value)
		}
		return true
	})
	written, _ := proto.MarshalOptions{Deterministic: true}.Marshal(kept.Interface())
	return written
}
