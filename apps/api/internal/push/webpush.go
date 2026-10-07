package push

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"syscall"
	"time"
)

// WebPush sends RFC 8030 push messages, encrypted per RFC 8291 (aes128gcm)
// and authenticated with VAPID (RFC 8292). UnifiedPush distributors such as
// ntfy accept exactly this format.
type WebPush struct {
	vapid   *ecdsa.PrivateKey
	subject string
	client  *http.Client
}

// NewWebPush creates a sender. subject is a mailto: or https: contact URI.
// A nil client gets SafeClient(false).
func NewWebPush(vapid *ecdsa.PrivateKey, subject string, client *http.Client) *WebPush {
	if client == nil {
		client = SafeClient(false)
	}
	return &WebPush{vapid: vapid, subject: subject, client: client}
}

// SafeClient returns an HTTP client for device-supplied push endpoints. Unless
// allowPrivate is set, it refuses to connect to loopback, private, link-local
// and other non-public addresses, checked after DNS resolution so a hostname
// cannot be used to reach internal services.
func SafeClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			if allowPrivate {
				return nil
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return err
			}
			if !IsPublic(ip.Unmap()) {
				return fmt.Errorf("%w: refusing to connect to non-public address %s", ErrInvalidTarget, ip)
			}
			return nil
		},
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext
	transport.Proxy = nil
	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
		// Never follow redirects: they could point anywhere.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// IsPublic reports whether ip is a globally routable unicast address.
func IsPublic(ip netip.Addr) bool {
	return ip.IsValid() && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() && !ip.IsMulticast() && !ip.IsUnspecified() &&
		!ip.IsInterfaceLocalMulticast() && !netip.MustParsePrefix("100.64.0.0/10").Contains(ip)
}

// PublicKey returns the VAPID application server key, base64url without padding.
// Devices pass it to their distributor when registering.
func (w *WebPush) PublicKey() string {
	pub, _ := w.vapid.PublicKey.ECDH()
	return base64.RawURLEncoding.EncodeToString(pub.Bytes())
}

// Subscription is a push endpoint and the receiver's encryption keys.
type Subscription struct {
	Endpoint string
	P256dh   string // receiver public key, base64url
	Auth     string // 16-byte auth secret, base64url
}

// Send encrypts payload for sub and delivers it with high urgency.
func (w *WebPush) Send(ctx context.Context, sub Subscription, payload []byte, ttl time.Duration) error {
	u, err := url.Parse(sub.Endpoint)
	if err != nil || u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("%w: endpoint is not a URL", ErrInvalidTarget)
	}
	uaPublic, err := decodeB64(sub.P256dh)
	if err != nil {
		return fmt.Errorf("%w: p256dh: %v", ErrInvalidTarget, err)
	}
	authSecret, err := decodeB64(sub.Auth)
	if err != nil || len(authSecret) != 16 {
		return fmt.Errorf("%w: auth secret must be 16 bytes", ErrInvalidTarget)
	}
	body, err := Encrypt(payload, uaPublic, authSecret)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTarget, err)
	}
	jwt, err := w.vapidJWT(u.Scheme + "://" + u.Host)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", strconv.Itoa(int(ttl.Seconds())))
	req.Header.Set("Urgency", "high")
	req.Header.Set("Authorization", "vapid t="+jwt+", k="+w.PublicKey())
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrGone
	case resp.StatusCode >= 300:
		return fmt.Errorf("push service returned %s", resp.Status)
	}
	return nil
}

func (w *WebPush) vapidJWT(audience string) (string, error) {
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(map[string]any{
		"aud": audience,
		"exp": time.Now().Add(12 * time.Hour).Unix(),
		"sub": w.subject,
	})
	if err != nil {
		return "", err
	}
	signingInput := header + "." + enc.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, w.vapid, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signingInput + "." + enc.EncodeToString(sig), nil
}

const recordSize = 4096

// Encrypt implements RFC 8291 message encryption with the aes128gcm content
// coding (RFC 8188), producing a single record.
func Encrypt(plaintext, uaPublic, authSecret []byte) ([]byte, error) {
	curve := ecdh.P256()
	ua, err := curve.NewPublicKey(uaPublic)
	if err != nil {
		return nil, fmt.Errorf("receiver key: %w", err)
	}
	as, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	shared, err := as.ECDH(ua)
	if err != nil {
		return nil, err
	}
	cek, nonce, err := deriveKeys(shared, authSecret, salt, ua.Bytes(), as.PublicKey().Bytes())
	if err != nil {
		return nil, err
	}
	if len(plaintext)+1+16 > recordSize {
		return nil, errors.New("payload too large for a single record")
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	padded := append(append([]byte{}, plaintext...), 0x02) // 0x02 marks the last record
	ciphertext := gcm.Seal(nil, nonce, padded, nil)

	asPublic := as.PublicKey().Bytes()
	out := make([]byte, 0, 16+4+1+len(asPublic)+len(ciphertext))
	out = append(out, salt...)
	out = binary.BigEndian.AppendUint32(out, recordSize)
	out = append(out, byte(len(asPublic)))
	out = append(out, asPublic...)
	return append(out, ciphertext...), nil
}

// Decrypt reverses Encrypt for the receiver. Bridge only needs it in tests,
// but keeping it next to Encrypt documents the format.
func Decrypt(body []byte, uaPrivate *ecdh.PrivateKey, authSecret []byte) ([]byte, error) {
	if len(body) < 21 {
		return nil, errors.New("body too short")
	}
	salt := body[:16]
	idLen := int(body[20])
	if len(body) < 21+idLen {
		return nil, errors.New("truncated header")
	}
	asPublicBytes := body[21 : 21+idLen]
	ciphertext := body[21+idLen:]
	as, err := ecdh.P256().NewPublicKey(asPublicBytes)
	if err != nil {
		return nil, err
	}
	shared, err := uaPrivate.ECDH(as)
	if err != nil {
		return nil, err
	}
	cek, nonce, err := deriveKeys(shared, authSecret, salt, uaPrivate.PublicKey().Bytes(), asPublicBytes)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	padded, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	i := bytes.LastIndexByte(padded, 0x02)
	if i < 0 {
		return nil, errors.New("missing padding delimiter")
	}
	return padded[:i], nil
}

func deriveKeys(shared, authSecret, salt, uaPublic, asPublic []byte) (cek, nonce []byte, err error) {
	prkKey, err := hkdf.Extract(sha256.New, shared, authSecret)
	if err != nil {
		return nil, nil, err
	}
	keyInfo := "WebPush: info\x00" + string(uaPublic) + string(asPublic)
	ikm, err := hkdf.Expand(sha256.New, prkKey, keyInfo, 32)
	if err != nil {
		return nil, nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, nil, err
	}
	if cek, err = hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16); err != nil {
		return nil, nil, err
	}
	nonce, err = hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	return cek, nonce, err
}

// GenerateVAPIDKey creates a new P-256 key and returns it with its PKCS#8 encoding.
func GenerateVAPIDKey() (*ecdsa.PrivateKey, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	return key, der, err
}

// ParseVAPIDKey parses a PKCS#8 P-256 private key.
func ParseVAPIDKey(der []byte) (*ecdsa.PrivateKey, error) {
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, err
	}
	key, ok := k.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, errors.New("VAPID key is not a P-256 ECDSA key")
	}
	return key, nil
}

func decodeB64(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("not valid base64")
}
