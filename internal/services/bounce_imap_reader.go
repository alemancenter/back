package services

import (
	"crypto/tls"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/alemancenter/fiber-api/pkg/logger"
	goImap "github.com/emersion/go-imap"
	imapClient "github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
	"go.uber.org/zap"
)

const (
	bounceProcessedFolder = "Processed"
	bounceInboxFolder     = "INBOX"
	bounceMaxRawBytes     = 256 * 1024 // 256 KB cap on raw message storage
)

// BounceIMAPReader connects to the bounce mailbox periodically, parses DSN
// messages, and delegates processing to BounceProcessorService.
//
// Safe concurrency: only one Run() executes at a time (guarded by mu).
// A trigger channel lets the HTTP "process-now" endpoint request an immediate
// run without spawning an uncontrolled goroutine.
//
// Config is read fresh from the global config on every cycle so that dashboard
// changes (host, port, password, enabled flag) take effect without a restart.
type BounceIMAPReader struct {
	processor *BounceProcessorService
	trigger   chan struct{}
	mu        sync.Mutex
	running   bool
}

func NewBounceIMAPReader(processor *BounceProcessorService) *BounceIMAPReader {
	return &BounceIMAPReader{
		processor: processor,
		trigger:   make(chan struct{}, 1),
	}
}

// liveCfg always returns the current bounce config from the global store.
func liveCfg() config.BounceMboxConfig { return config.Get().Mail.Bounce }

// TriggerNow requests an immediate processing cycle from the HTTP layer.
// Returns false if a trigger is already queued (idempotent).
func (r *BounceIMAPReader) TriggerNow() bool {
	select {
	case r.trigger <- struct{}{}:
		return true
	default:
		return false
	}
}

// StartBounceIMAPScheduler starts a background goroutine that runs every 10 minutes
// and also responds to manual triggers via TriggerNow().
// The goroutine always starts; the enabled flag is checked on every cycle so that
// enabling/disabling from the dashboard takes effect without a restart.
func StartBounceIMAPScheduler(reader *BounceIMAPReader) {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()

		// Run once immediately on startup (run() will skip if disabled).
		reader.runSafe()

		for {
			select {
			case <-ticker.C:
				reader.runSafe()
			case <-reader.trigger:
				reader.runSafe()
			}
		}
	}()

	logger.Info("bounce IMAP scheduler started (checks every 10 min)")
}

// runSafe prevents concurrent executions via a mutex.
func (r *BounceIMAPReader) runSafe() {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()

	r.run()
}

func (r *BounceIMAPReader) run() {
	// Always read fresh config so dashboard changes take effect without restart.
	cfg := liveCfg()

	if !cfg.Enabled {
		return
	}
	if cfg.Host == "" || cfg.Username == "" || cfg.Password == "" {
		logger.Warn("bounce IMAP: configuration incomplete — host/username/password required")
		return
	}

	// Port 465 is the SMTP-SSL port — not valid for IMAP.
	// Warn clearly and abort so the log message guides the admin.
	if cfg.Port == 465 {
		logger.Error("bounce IMAP: port 465 is the SMTP-SSL port, NOT an IMAP port. " +
			"Use 993 (IMAPS / implicit TLS) or 143 (IMAP / STARTTLS). " +
			"Change BOUNCE_IMAP_PORT in the dashboard settings.")
		return
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	tlsCfg := &tls.Config{ServerName: cfg.Host}

	var c *imapClient.Client
	var err error

	switch {
	case cfg.TLS && cfg.Port != 143:
		// Implicit TLS — standard port 993 (IMAPS).
		c, err = imapClient.DialTLS(addr, tlsCfg)
	case cfg.TLS && cfg.Port == 143:
		// STARTTLS — connects plain on 143, then upgrades.
		c, err = imapClient.Dial(addr)
		if err == nil {
			err = c.StartTLS(tlsCfg)
		}
	default:
		// Plain IMAP — no encryption (not recommended for production).
		c, err = imapClient.Dial(addr)
	}

	if err != nil {
		logger.Error("bounce IMAP: dial failed",
			zap.String("addr", addr),
			zap.Bool("tls", cfg.TLS),
			zap.String("hint", "for port 993 set TLS=true; for port 143 with STARTTLS set TLS=true; port 465 is SMTP only"),
			zap.Error(err),
		)
		return
	}
	defer c.Logout() //nolint:errcheck

	if err = c.Login(cfg.Username, cfg.Password); err != nil {
		logger.Error("bounce IMAP: login failed", zap.String("user", cfg.Username), zap.Error(err))
		return
	}

	mbox, err := c.Select(bounceInboxFolder, false)
	if err != nil {
		logger.Error("bounce IMAP: select INBOX failed", zap.Error(err))
		return
	}
	if mbox.Messages == 0 {
		return
	}

	seqSet := new(goImap.SeqSet)
	seqSet.AddRange(1, mbox.Messages)

	section := &goImap.BodySectionName{}
	items := []goImap.FetchItem{goImap.FetchEnvelope, goImap.FetchUid, section.FetchItem()}

	messages := make(chan *goImap.Message, 20)
	fetchDone := make(chan error, 1)
	go func() { fetchDone <- c.Fetch(seqSet, items, messages) }()

	var processedSeqs []uint32

	for msg := range messages {
		body := msg.GetBody(section)
		if body == nil {
			continue
		}

		// Read raw bytes (capped to avoid storing huge messages).
		raw, _ := io.ReadAll(io.LimitReader(body, bounceMaxRawBytes))
		rawStr := string(raw)

		// Parse as RFC 2822 message to extract headers and DSN parts.
		input := r.parseDSN(rawStr, msg.Envelope)
		if input.RecipientEmail == "" {
			continue
		}

		r.processor.Process(input)
		processedSeqs = append(processedSeqs, msg.SeqNum)
	}

	if err = <-fetchDone; err != nil {
		logger.Error("bounce IMAP: fetch error", zap.Error(err))
	}

	if len(processedSeqs) == 0 {
		return
	}

	// Ensure the Processed folder exists before moving.
	_ = c.Create(bounceProcessedFolder)

	processed := new(goImap.SeqSet)
	for _, s := range processedSeqs {
		processed.AddNum(s)
	}
	if err = c.Move(processed, bounceProcessedFolder); err != nil {
		logger.Warn("bounce IMAP: move to Processed failed (messages will be re-processed but dedup prevents double-counting)",
			zap.Error(err),
		)
	}

	logger.Info("bounce IMAP: cycle complete",
		zap.Int("processed", len(processedSeqs)),
	)
}

// parseDSN extracts recipient, SMTP status, and diagnostic code from the raw
// message text.  It handles:
//   - Folded (multi-line) RFC 2822 headers
//   - Multipart/report DSN body parts
//   - Plain-text fallback when no structured DSN is present
func (r *BounceIMAPReader) parseDSN(raw string, envelope *goImap.Envelope) ProcessBounceInput {
	var input ProcessBounceInput

	// Extract Message-ID from IMAP envelope for deduplication.
	if envelope != nil {
		input.MessageID = strings.Trim(envelope.MessageId, "<>")
	}

	// Unfold RFC 2822 folded headers (CRLF + whitespace → single space).
	unfolded := unfoldHeaders(raw)
	lines := strings.Split(unfolded, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		switch {
		case strings.HasPrefix(lower, "final-recipient:"):
			// Final-Recipient: rfc822; user@example.com
			if parts := strings.SplitN(trimmed, ";", 2); len(parts) == 2 {
				input.RecipientEmail = strings.TrimSpace(parts[1])
			}

		case strings.HasPrefix(lower, "original-recipient:") && input.RecipientEmail == "":
			if parts := strings.SplitN(trimmed, ";", 2); len(parts) == 2 {
				input.RecipientEmail = strings.TrimSpace(parts[1])
			}

		case strings.HasPrefix(lower, "status:"):
			// Status: 5.1.1
			input.SmtpStatus = strings.TrimSpace(trimmed[7:])

		case strings.HasPrefix(lower, "diagnostic-code:"):
			// Diagnostic-Code: smtp; 550-5.1.1 The email account that you tried to reach does not exist
			rest := trimmed[len("diagnostic-code:"):]
			if idx := strings.Index(rest, ";"); idx >= 0 {
				input.DiagnosticCode = strings.TrimSpace(rest[idx+1:])
			} else {
				input.DiagnosticCode = strings.TrimSpace(rest)
			}
		}
	}

	// Fallback: try to extract the recipient from the go-message library for
	// multipart/report messages when the simple line parser above found nothing.
	if input.RecipientEmail == "" {
		input.RecipientEmail = extractRecipientFromMIME(raw)
	}

	// Fallback: if no diagnostic code found, scan raw text for known error signals.
	if input.DiagnosticCode == "" {
		input.DiagnosticCode = detectSignalInRaw(raw)
	}

	input.RawMessage = raw
	return input
}

// unfoldHeaders replaces folded header continuations with a single space so
// every logical header fits on one line.
func unfoldHeaders(raw string) string {
	// RFC 2822 folding: CRLF followed by whitespace.
	result := strings.ReplaceAll(raw, "\r\n ", " ")
	result = strings.ReplaceAll(result, "\r\n\t", " ")
	result = strings.ReplaceAll(result, "\n ", " ")
	result = strings.ReplaceAll(result, "\n\t", " ")
	return result
}

// extractRecipientFromMIME uses go-message to walk the MIME parts of a
// multipart/report and look for a message/delivery-status part.
func extractRecipientFromMIME(raw string) string {
	r2, err := mail.CreateReader(strings.NewReader(raw))
	if err != nil {
		return ""
	}
	for {
		part, err := r2.NextPart()
		if err != nil {
			break
		}
		ct := part.Header.Get("Content-Type")
		if !strings.HasPrefix(strings.ToLower(ct), "message/delivery-status") {
			continue
		}
		dsn, _ := io.ReadAll(part.Body)
		unfolded := unfoldHeaders(string(dsn))
		for _, line := range strings.Split(unfolded, "\n") {
			lower := strings.ToLower(strings.TrimSpace(line))
			if strings.HasPrefix(lower, "final-recipient:") {
				if parts := strings.SplitN(strings.TrimSpace(line), ";", 2); len(parts) == 2 {
					return strings.TrimSpace(parts[1])
				}
			}
		}
	}
	return ""
}

// detectSignalInRaw is a last-resort scan of the raw message body for known
// bounce signal strings when no structured DSN headers were found.
func detectSignalInRaw(raw string) string {
	lower := strings.ToLower(raw)
	for _, sig := range append(hardBounceSignals, softBounceSignals...) {
		if strings.Contains(lower, sig) {
			return sig
		}
	}
	return ""
}
