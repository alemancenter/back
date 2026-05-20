package services

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/config"
	mail "github.com/wneessen/go-mail"
)

// MailService handles email sending
type MailService struct {
	cfg config.MailConfig
}

// NewMailService creates a new MailService using the global configuration.
func NewMailService() *MailService {
	return &MailService{cfg: config.Get().Mail}
}

// NewMailServiceWithConfig creates a MailService with the given configuration.
// Used when testing SMTP settings submitted from the dashboard before they are persisted.
func NewMailServiceWithConfig(cfg config.MailConfig) *MailService {
	return &MailService{cfg: cfg}
}

// Send sends a plain text or HTML email.
func (m *MailService) Send(to, subject, body string, isHTML bool) error {
	msg := mail.NewMsg()

	fromName := strings.TrimSpace(m.cfg.FromName)
	fromAddress := strings.TrimSpace(m.cfg.FromAddress)

	if fromName == "" {
		fromName = fromAddress
	}

	if err := msg.From(fmt.Sprintf("%s <%s>", fromName, fromAddress)); err != nil {
		return fmt.Errorf("invalid from address: %w", MapError(err))
	}

	// Set SMTP envelope sender (MAIL FROM) to the bounce mailbox so delivery-failure
	// notifications (DSN) are routed to bounce@... and not to the From address.
	// We always read from the live global config (not m.cfg) so a dashboard update
	// takes effect on the next send without requiring a server restart.
	liveBounce := strings.TrimSpace(config.Get().Mail.BounceAddress)
	if liveBounce == "" {
		liveBounce = strings.TrimSpace(m.cfg.BounceAddress)
	}
	if liveBounce != "" {
		if err := msg.EnvelopeFrom(liveBounce); err != nil {
			return fmt.Errorf("invalid bounce address: %w", MapError(err))
		}
	}

	if err := msg.To(to); err != nil {
		return fmt.Errorf("invalid to address: %w", MapError(err))
	}

	msg.Subject(subject)

	if isHTML {
		msg.SetBodyString(mail.TypeTextHTML, body)
	} else {
		msg.SetBodyString(mail.TypeTextPlain, body)
	}

	opts := []mail.Option{
		mail.WithPort(m.cfg.Port),
		mail.WithSMTPAuth(mail.SMTPAuthPlain),
		mail.WithUsername(m.cfg.Username),
		mail.WithPassword(m.cfg.Password),
		mail.WithTimeout(20 * time.Second),
	}

	switch strings.ToLower(strings.TrimSpace(m.cfg.Encryption)) {
	case "ssl":
		// Implicit SSL/TLS — usually port 465.
		opts = append(opts, mail.WithSSLPort(false))

	case "tls", "starttls":
		// STARTTLS — usually port 587.
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))

	case "none":
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))

	default:
		opts = append(opts, mail.WithTLSPolicy(mail.TLSOpportunistic))
	}

	client, err := mail.NewClient(m.cfg.Host, opts...)
	if err != nil {
		return fmt.Errorf("failed to create mail client: %w", MapError(err))
	}

	if err := client.DialAndSend(msg); err != nil {
		return fmt.Errorf("failed to send mail: %w", MapError(err))
	}

	return nil
}

// SendVerificationEmail sends an email verification link.
func (m *MailService) SendVerificationEmail(to, name, verificationURL string) error {
	brandName := m.brandName()

	subject := fmt.Sprintf("تأكيد البريد الإلكتروني - %s", brandName)

	body := buildArabicEmailTemplate(emailTemplateData{
		BrandName:    brandName,
		Preheader:    "تأكيد البريد الإلكتروني",
		Greeting:     fmt.Sprintf("مرحباً %s،", sanitizeText(name, "عزيزي المستخدم")),
		Title:        "أهلاً بك في منصتنا",
		Message:      "يسعدنا انضمامك إلينا. تم إنشاء حسابك بنجاح، ولم يتبقَّ سوى تأكيد بريدك الإلكتروني لتفعيل الحساب والاستفادة من خدمات الموقع بشكل كامل وآمن.",
		ButtonText:   "تأكيد البريد الإلكتروني",
		ActionURL:    verificationURL,
		ExpiryNote:   "هذا الرابط صالح لمدة 24 ساعة. إذا انتهت صلاحية الرابط، يمكنك طلب رابط جديد من صفحة تسجيل الدخول.",
		SecurityNote: "إذا لم تقم بإنشاء حساب لدينا، يمكنك تجاهل هذه الرسالة بأمان.",
		FooterNote:   "نحن نهتم بحماية حسابك وتقديم تجربة موثوقة وآمنة.",
	})

	return m.Send(to, subject, body, true)
}

// SendPasswordResetEmail sends a password reset link.
func (m *MailService) SendPasswordResetEmail(to, name, resetURL string) error {
	brandName := m.brandName()

	subject := fmt.Sprintf("إعادة تعيين كلمة المرور - %s", brandName)

	body := buildArabicEmailTemplate(emailTemplateData{
		BrandName:    brandName,
		Preheader:    "إعادة تعيين كلمة المرور",
		Greeting:     fmt.Sprintf("مرحباً %s،", sanitizeText(name, "عزيزي المستخدم")),
		Title:        "طلب إعادة تعيين كلمة المرور",
		Message:      "تلقينا طلباً لإعادة تعيين كلمة المرور الخاصة بحسابك. للحفاظ على أمان حسابك، يرجى استخدام الزر أدناه لإتمام العملية من خلال الرابط الآمن.",
		ButtonText:   "إعادة تعيين كلمة المرور",
		ActionURL:    resetURL,
		ExpiryNote:   "هذا الرابط صالح لمدة 60 دقيقة فقط. بعد انتهاء المدة، ستحتاج إلى طلب رابط جديد.",
		SecurityNote: "إذا لم تطلب إعادة تعيين كلمة المرور، يرجى تجاهل هذه الرسالة. سيبقى حسابك آمناً ولن يتم تغيير كلمة المرور.",
		FooterNote:   "تم إرسال هذه الرسالة لحماية حسابك من أي وصول غير مصرح به.",
	})

	return m.Send(to, subject, body, true)
}

// TestSMTP tests the SMTP connection.
func (m *MailService) TestSMTP() error {
	brandName := m.brandName()

	return m.Send(
		m.cfg.FromAddress,
		fmt.Sprintf("SMTP Test - %s", brandName),
		"SMTP connection test successful.",
		false,
	)
}

// brandName returns the site/brand name from the active mail configuration.
// In the dashboard, mail_from_name should be synchronized with site_name.
func (m *MailService) brandName() string {
	name := strings.TrimSpace(m.cfg.FromName)
	if name != "" {
		return html.EscapeString(name)
	}

	address := strings.TrimSpace(m.cfg.FromAddress)
	if address != "" {
		return html.EscapeString(address)
	}

	return "الموقع"
}

type emailTemplateData struct {
	BrandName    string
	Preheader    string
	Greeting     string
	Title        string
	Message      string
	ButtonText   string
	ActionURL    string
	ExpiryNote   string
	SecurityNote string
	FooterNote   string
}

func buildArabicEmailTemplate(data emailTemplateData) string {
	brandName := html.EscapeString(strings.TrimSpace(data.BrandName))
	preheader := html.EscapeString(strings.TrimSpace(data.Preheader))
	greeting := html.EscapeString(strings.TrimSpace(data.Greeting))
	title := html.EscapeString(strings.TrimSpace(data.Title))
	message := html.EscapeString(strings.TrimSpace(data.Message))
	buttonText := html.EscapeString(strings.TrimSpace(data.ButtonText))
	actionURL := html.EscapeString(strings.TrimSpace(data.ActionURL))
	expiryNote := html.EscapeString(strings.TrimSpace(data.ExpiryNote))
	securityNote := html.EscapeString(strings.TrimSpace(data.SecurityNote))
	footerNote := html.EscapeString(strings.TrimSpace(data.FooterNote))

	if brandName == "" {
		brandName = "الموقع"
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html dir="rtl" lang="ar">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>%s</title>
</head>
<body style="margin:0; padding:0; background-color:#f3f6fb; font-family:Arial,Tahoma,sans-serif; direction:rtl; text-align:right; color:#1f2937;">

  <div style="display:none; max-height:0; overflow:hidden; opacity:0; color:transparent;">
    %s
  </div>

  <table role="presentation" width="100%%" cellspacing="0" cellpadding="0" border="0" style="background-color:#f3f6fb; margin:0; padding:24px 12px;">
    <tr>
      <td align="center">

        <table role="presentation" width="100%%" cellspacing="0" cellpadding="0" border="0" style="max-width:640px; background-color:#ffffff; border-radius:16px; overflow:hidden; box-shadow:0 10px 30px rgba(15,23,42,0.08);">

          <tr>
            <td style="background-color:#0d6efd; padding:28px 32px; text-align:right;">
              <div style="font-size:24px; font-weight:700; color:#ffffff; line-height:1.5;">
                %s
              </div>
              <div style="font-size:14px; color:#dbeafe; margin-top:6px; line-height:1.8;">
                نهتم بتجربة المستخدم، جودة الخدمة، وأمان الحسابات.
              </div>
            </td>
          </tr>

          <tr>
            <td style="padding:34px 32px 10px 32px;">

              <h1 style="margin:0 0 14px 0; font-size:24px; line-height:1.5; color:#111827; font-weight:700;">
                %s
              </h1>

              <p style="margin:0 0 16px 0; font-size:16px; line-height:1.9; color:#374151;">
                %s
              </p>

              <p style="margin:0 0 22px 0; font-size:16px; line-height:1.9; color:#374151;">
                %s
              </p>

              <table role="presentation" cellspacing="0" cellpadding="0" border="0" width="100%%" style="margin:30px 0;">
                <tr>
                  <td align="center">
                    <a href="%s"
                       target="_blank"
                       rel="noopener"
                       style="display:inline-block; background-color:#0d6efd; color:#ffffff; text-decoration:none; padding:14px 34px; border-radius:10px; font-size:16px; font-weight:700; line-height:1.4;">
                      %s
                    </a>
                  </td>
                </tr>
              </table>

              <div style="background-color:#f8fafc; border:1px solid #e5e7eb; border-radius:12px; padding:16px 18px; margin:0 0 18px 0;">
                <p style="margin:0; font-size:14px; line-height:1.9; color:#4b5563;">
                  %s
                </p>
              </div>

              <p style="margin:0 0 22px 0; font-size:14px; line-height:1.9; color:#6b7280;">
                %s
              </p>

              <p style="margin:0; font-size:14px; line-height:1.9; color:#6b7280;">
                إذا لم يعمل الزر، يمكنك نسخ الرابط التالي وفتحه في المتصفح:
              </p>

              <p style="margin:8px 0 0 0; word-break:break-all; direction:ltr; text-align:left; font-size:13px; line-height:1.7; color:#2563eb;">
                %s
              </p>

            </td>
          </tr>

          <tr>
            <td style="padding:24px 32px 30px 32px;">
              <div style="border-top:1px solid #e5e7eb; padding-top:18px;">
                <p style="margin:0; font-size:13px; line-height:1.8; color:#9ca3af;">
                  هذه رسالة تلقائية من %s. يرجى عدم الرد مباشرة على هذا البريد.
                </p>
                <p style="margin:6px 0 0 0; font-size:13px; line-height:1.8; color:#9ca3af;">
                  %s
                </p>
              </div>
            </td>
          </tr>

        </table>

      </td>
    </tr>
  </table>

</body>
</html>`,
		preheader,
		preheader,
		brandName,
		greeting,
		title,
		message,
		actionURL,
		buttonText,
		expiryNote,
		securityNote,
		actionURL,
		brandName,
		footerNote,
	)
}

func sanitizeText(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return html.EscapeString(fallback)
	}

	return html.EscapeString(value)
}
