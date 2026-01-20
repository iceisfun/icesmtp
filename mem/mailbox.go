package mem

import (
	"context"
	"strings"
	"sync"

	"github.com/iceisfun/icesmtp"
)

// Mailbox is an in-memory Mailbox implementation using a static registry.
// Addresses can be added and removed dynamically.
type Mailbox struct {
	mu        sync.RWMutex
	addresses map[icesmtp.EmailAddress]*MailboxEntry
	domains   map[icesmtp.Domain]bool
	catchAll  bool // Accept any address at registered domains
}

// MailboxEntry represents a mailbox in the registry.
type MailboxEntry struct {
	// Address is the full email address.
	Address icesmtp.EmailAddress

	// Enabled indicates if the mailbox can receive mail.
	Enabled bool

	// Aliases lists addresses that forward to this mailbox.
	Aliases []icesmtp.EmailAddress
}

// NewMailbox creates a new in-memory mailbox registry.
func NewMailbox() *Mailbox {
	return &Mailbox{
		addresses: make(map[icesmtp.EmailAddress]*MailboxEntry),
		domains:   make(map[icesmtp.Domain]bool),
	}
}

// NewMailboxWithDomains creates a mailbox registry with accepted domains.
func NewMailboxWithDomains(domains ...icesmtp.Domain) *Mailbox {
	m := NewMailbox()
	for _, d := range domains {
		m.AddDomain(d)
	}
	return m
}

// AddAddress adds an address to the registry.
func (m *Mailbox) AddAddress(address icesmtp.EmailAddress) {
	m.mu.Lock()
	defer m.mu.Unlock()

	addr := strings.ToLower(address)
	m.addresses[addr] = &MailboxEntry{
		Address: addr,
		Enabled: true,
	}

	// Also register the domain
	if idx := strings.LastIndex(addr, "@"); idx != -1 {
		domain := addr[idx+1:]
		m.domains[domain] = true
	}
}

// AddAddresses adds multiple addresses to the registry.
func (m *Mailbox) AddAddresses(addresses ...icesmtp.EmailAddress) {
	for _, addr := range addresses {
		m.AddAddress(addr)
	}
}

// RemoveAddress removes an address from the registry.
func (m *Mailbox) RemoveAddress(address icesmtp.EmailAddress) {
	m.mu.Lock()
	defer m.mu.Unlock()

	addr := strings.ToLower(address)
	delete(m.addresses, addr)
}

// AddDomain adds an accepted domain.
func (m *Mailbox) AddDomain(domain icesmtp.Domain) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.domains[strings.ToLower(domain)] = true
}

// RemoveDomain removes an accepted domain.
func (m *Mailbox) RemoveDomain(domain icesmtp.Domain) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.domains, strings.ToLower(domain))
}

// SetCatchAll enables or disables catch-all mode.
// When enabled, any address at a registered domain is accepted.
func (m *Mailbox) SetCatchAll(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.catchAll = enabled
}

// ValidateRecipient checks if a recipient is valid.
func (m *Mailbox) ValidateRecipient(ctx context.Context, recipient icesmtp.MailPath, session icesmtp.SessionInfo) icesmtp.RecipientResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	addr := strings.ToLower(recipient.Address)

	// Check if address is explicitly registered
	if entry, ok := m.addresses[addr]; ok {
		if !entry.Enabled {
			return icesmtp.RecipientResult{
				Path:   recipient,
				Status: icesmtp.RecipientRejected,
				Response: icesmtp.NewResponse(icesmtp.Reply550MailboxUnavailable,
					"Mailbox disabled"),
			}
		}
		return icesmtp.RecipientResult{
			Path:     recipient,
			Status:   icesmtp.RecipientAccepted,
			Response: icesmtp.ResponseOK,
		}
	}

	// Check catch-all for registered domains
	if m.catchAll {
		if idx := strings.LastIndex(addr, "@"); idx != -1 {
			domain := addr[idx+1:]
			if m.domains[domain] {
				return icesmtp.RecipientResult{
					Path:     recipient,
					Status:   icesmtp.RecipientAccepted,
					Response: icesmtp.ResponseOK,
				}
			}
		}
	}

	// Check if domain is registered (for better error messages)
	if idx := strings.LastIndex(addr, "@"); idx != -1 {
		domain := addr[idx+1:]
		if !m.domains[domain] {
			return icesmtp.RecipientResult{
				Path:   recipient,
				Status: icesmtp.RecipientRejected,
				Response: icesmtp.NewResponse(icesmtp.Reply550MailboxUnavailable,
					"Domain not handled by this server"),
			}
		}
	}

	return icesmtp.RecipientResult{
		Path:   recipient,
		Status: icesmtp.RecipientRejected,
		Response: icesmtp.NewResponse(icesmtp.Reply550MailboxUnavailable,
			"No such user"),
	}
}

// Exists checks if a mailbox exists.
func (m *Mailbox) Exists(ctx context.Context, address icesmtp.EmailAddress) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	addr := strings.ToLower(address)
	_, ok := m.addresses[addr]
	return ok, nil
}

// CanReceive checks if the mailbox can currently receive mail.
func (m *Mailbox) CanReceive(ctx context.Context, address icesmtp.EmailAddress) (bool, icesmtp.MailboxStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	addr := strings.ToLower(address)
	entry, ok := m.addresses[addr]

	if !ok {
		return false, icesmtp.MailboxStatusNotFound, nil
	}

	if !entry.Enabled {
		return false, icesmtp.MailboxStatusDisabled, nil
	}

	return true, icesmtp.MailboxStatusOK, nil
}

// ListAddresses returns all registered addresses.
func (m *Mailbox) ListAddresses() []icesmtp.EmailAddress {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]icesmtp.EmailAddress, 0, len(m.addresses))
	for addr := range m.addresses {
		result = append(result, addr)
	}
	return result
}

// ListDomains returns all accepted domains.
func (m *Mailbox) ListDomains() []icesmtp.Domain {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]icesmtp.Domain, 0, len(m.domains))
	for domain := range m.domains {
		result = append(result, domain)
	}
	return result
}

// DomainPolicy is an in-memory implementation of icesmtp.DomainPolicy.
type DomainPolicy struct {
	mailbox *Mailbox
}

// NewDomainPolicy creates a DomainPolicy backed by a Mailbox.
func NewDomainPolicy(mailbox *Mailbox) *DomainPolicy {
	return &DomainPolicy{mailbox: mailbox}
}

// IsLocalDomain checks if a domain is local.
func (p *DomainPolicy) IsLocalDomain(ctx context.Context, domain icesmtp.Domain) (bool, error) {
	p.mailbox.mu.RLock()
	defer p.mailbox.mu.RUnlock()

	_, ok := p.mailbox.domains[strings.ToLower(domain)]
	return ok, nil
}

// AcceptedDomains returns all accepted domains.
func (p *DomainPolicy) AcceptedDomains(ctx context.Context) ([]icesmtp.Domain, error) {
	return p.mailbox.ListDomains(), nil
}

// RelayAllowed checks if relaying is allowed.
// By default, relaying is not allowed for non-local domains.
func (p *DomainPolicy) RelayAllowed(ctx context.Context, domain icesmtp.Domain, session icesmtp.SessionInfo) (bool, error) {
	// Allow relay for authenticated users
	if session.Authenticated() {
		return true, nil
	}
	// Otherwise, only allow for local domains
	return p.IsLocalDomain(ctx, domain)
}

// Ensure Mailbox implements the interfaces.
var (
	_ icesmtp.Mailbox         = (*Mailbox)(nil)
	_ icesmtp.MailboxExtended = (*Mailbox)(nil)
	_ icesmtp.DomainPolicy    = (*DomainPolicy)(nil)
)
