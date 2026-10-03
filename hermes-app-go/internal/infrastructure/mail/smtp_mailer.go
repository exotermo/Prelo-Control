// Package mail sends the two dashboard-auth emails (activation link, password-reset link) over
// SMTP — same MESSAGING_SMTP_* naming convention as messaging-core's DashboardMailer, so both
// services can point at the same Mailpit container in dev and the same real SMTP provider in
// production.
package mail

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

type Config struct {
	Host      string
	Port      string
	User      string
	Password  string
	Auth      bool
	StartTLS  bool
	From      string
	PublicURL string
}

// Configured mirrors DashboardMailer.configured(): sender, from address and public URL must all
// be set, or dashboard-auth emails are skipped (logged, not sent) instead of failing the request.
func (c Config) Configured() bool {
	return strings.TrimSpace(c.Host) != "" && strings.TrimSpace(c.From) != "" && strings.TrimSpace(c.PublicURL) != ""
}

type SMTPMailer struct {
	cfg Config
}

func NewSMTPMailer(cfg Config) *SMTPMailer {
	return &SMTPMailer{cfg: cfg}
}

func (m *SMTPMailer) SendActivation(to, token string) error {
	link := fmt.Sprintf("%s/activate?token=%s", strings.TrimRight(m.cfg.PublicURL, "/"), token)
	return m.send(to, "Ative seu acesso ao Hermes", "Abra o link para continuar: "+link+
		"\n\nSe não solicitou isto, ignore a mensagem. O link expira e só pode ser usado uma vez.")
}

func (m *SMTPMailer) SendPasswordReset(to, token string) error {
	link := fmt.Sprintf("%s/reset-password?token=%s", strings.TrimRight(m.cfg.PublicURL, "/"), token)
	return m.send(to, "Recuperação de senha - Hermes", "Abra o link para definir uma nova senha: "+link+
		"\n\nSe não solicitou isto, ignore a mensagem. O link expira e só pode ser usado uma vez.")
}

func (m *SMTPMailer) send(to, subject, body string) error {
	if !m.cfg.Configured() {
		return nil
	}
	addr := net.JoinHostPort(m.cfg.Host, m.cfg.Port)
	client, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer client.Close()

	if m.cfg.StartTLS {
		if err := client.StartTLS(&tls.Config{ServerName: m.cfg.Host}); err != nil {
			return err
		}
	}
	if m.cfg.Auth {
		auth := smtp.PlainAuth("", m.cfg.User, m.cfg.Password, m.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(m.cfg.From); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", m.cfg.From, to, subject, body)
	if _, err := writer.Write([]byte(message)); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}
