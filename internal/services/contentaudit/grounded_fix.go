package contentaudit

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
	"github.com/imanjo/fiber-api/internal/utils"
)

var (
	ErrGroundedSourceInsufficient = errors.New("grounded repair source evidence is insufficient")
	ErrGroundedValidationFailed   = errors.New("grounded repair validation failed")
	ErrUngroundedFixPreview       = errors.New("fix preview was not produced by grounded repair v2")
	ErrGroundedAIUnavailable      = errors.New("grounded repair AI provider unavailable")
)

const (
	groundingSummaryPrefix = "[GROUNDING_V2]"
	groundingMinScore      = 90
	groundingPromptVersion = "grounded-content-repair-v2"
	groundingReadLimit     = int64(64 * 1024)
	groundingEvidenceLimit = 12000
)

type groundedEvidence struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Text     string `json:"text"`
	Verified bool   `json:"verified"`
}

type groundedSourcePack struct {
	ContentType string             `json:"content_type"`
	ContentID   uint               `json:"content_id"`
	CountryCode string             `json:"country_code"`
	Title       string             `json:"title"`
	URL         string             `json:"url"`
	Curriculum  string             `json:"curriculum"`
	Evidence    []groundedEvidence `json:"evidence"`
}

type groundedFact struct {
	Claim       string   `json:"claim"`
	EvidenceIDs []string `json:"evidence_ids"`
	Confidence  int      `json:"confidence"`
}

type groundedFactExtraction struct {
	Purpose            string         `json:"purpose"`
	Audience           []string       `json:"audience"`
	Facts              []groundedFact `json:"facts"`
	InsufficientSource bool           `json:"insufficient_source"`
	SourceNotes        []string       `json:"source_notes"`
}

type groundedDraft struct {
	Title           string `json:"title"`
	ContentHTML     string `json:"content_html"`
	UsedFactIndexes []int  `json:"used_fact_indexes"`
}

type groundedValidation struct {
	GroundingScore    int      `json:"grounding_score"`
	SupportedClaims   int      `json:"supported_claims"`
	UnsupportedClaims []string `json:"unsupported_claims"`
	Notes             []string `json:"notes"`
}

type groundingSummaryMeta struct {
	Status      string
	Score       int
	Evidence    int
	Unsupported int
	Model       string
}

type groundedLoadedContent struct {
	Type              string
	ID                uint
	CountryCode       string
	Title             string
	Content           string
	URL               string
	GradeName         string
	SubjectName       string
	SemesterName      string
	CategoryName      string
	CurriculumContext string
	Files             []models.File
}

func (s *Service) CreateGroundedFixPreview(ctx context.Context, decisionID uint64) (*models.ContentAIFixPreview, error) {
	decision, err := s.repo.GetAIDecision(ctx, decisionID)
	if err != nil {
		return nil, err
	}

	content, err := s.loadGroundedContent(ctx, decision)
	if err != nil {
		return nil, err
	}

	pack := buildGroundedSourcePack(content)
	if len(pack.Evidence) == 0 {
		return nil, ErrGroundedSourceInsufficient
	}

	facts, modelName, err := runGroundedFactExtraction(ctx, pack)
	if err != nil {
		return nil, err
	}
	facts = sanitizeGroundedFacts(facts, pack)
	if facts.InsufficientSource || len(facts.Facts) == 0 {
		return nil, fmt.Errorf("%w: لا توجد أدلة كافية لإنشاء نص موثوق؛ أضف وصفًا أو مرفقًا قابلًا للقراءة ثم أعد المحاولة", ErrGroundedSourceInsufficient)
	}

	draft, writerModel, err := runGroundedWriter(ctx, pack, facts)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(writerModel) != "" {
		modelName = writerModel
	}
	draft.Title = strings.TrimSpace(draft.Title)
	if draft.Title == "" {
		draft.Title = content.Title
	}
	draft.ContentHTML = normalizeFixedHTML(draft.ContentHTML)
	if strings.TrimSpace(normalizePlainText(draft.ContentHTML)) == "" || strings.EqualFold(normalizePlainText(draft.ContentHTML), normalizePlainText(content.Content)) {
		return nil, fmt.Errorf("%w: المسودة الجديدة لا تضيف قيمة موثقة كافية", ErrGroundedValidationFailed)
	}
	if !validUsedFactIndexes(draft.UsedFactIndexes, len(facts.Facts)) {
		return nil, fmt.Errorf("%w: المسودة تشير إلى حقائق غير موجودة في حزمة الأدلة", ErrGroundedValidationFailed)
	}

	validation, validatorModel, err := runGroundedValidator(ctx, pack, facts, draft)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(validatorModel) != "" {
		modelName = validatorModel
	}
	validation.GroundingScore = clamp(validation.GroundingScore)
	validation.UnsupportedClaims = compactStrings(validation.UnsupportedClaims)
	if validation.GroundingScore < groundingMinScore || len(validation.UnsupportedClaims) > 0 {
		return nil, fmt.Errorf("%w: grounding_score=%d unsupported_claims=%d", ErrGroundedValidationFailed, validation.GroundingScore, len(validation.UnsupportedClaims))
	}

	summary := buildGroundingSummary(validation, len(pack.Evidence), modelName, facts, pack)
	preview := &models.ContentAIFixPreview{
		DecisionID:      decision.ID,
		ContentType:     normalizeContentType(decision.ContentType),
		ContentID:       fmt.Sprintf("%s:%d", content.CountryCode, content.ID),
		CountryCode:     content.CountryCode,
		OriginalTitle:   content.Title,
		OriginalContent: content.Content,
		FixedTitle:      draft.Title,
		FixedContent:    draft.ContentHTML,
		FixSummary:      summary,
		Status:          models.AIFixStatusPreviewed,
	}
	if err := s.repo.SaveFixPreview(ctx, preview); err != nil {
		return nil, fmt.Errorf("save grounded fix preview failed: %w", err)
	}
	return preview, nil
}

func (s *Service) ApplyGroundedFix(ctx context.Context, previewID uint64, userID *uint, note string) (*models.ContentAIFixPreview, error) {
	preview, err := s.repo.GetFixPreview(ctx, previewID)
	if err != nil {
		return nil, err
	}
	if preview.Status != models.AIFixStatusPreviewed {
		return nil, ErrFixAlreadyClosed
	}
	meta, ok := parseGroundingSummary(preview.FixSummary)
	if !ok {
		return nil, ErrUngroundedFixPreview
	}
	if meta.Status != "grounded" || meta.Score < groundingMinScore || meta.Unsupported != 0 {
		return nil, fmt.Errorf("%w: score=%d unsupported=%d", ErrGroundedValidationFailed, meta.Score, meta.Unsupported)
	}
	if strings.TrimSpace(normalizePlainText(preview.FixedContent)) == "" {
		return nil, fmt.Errorf("%w: empty grounded draft", ErrGroundedValidationFailed)
	}

	_, _, id := normalizeContentReference(preview.ContentID, preview.CountryCode)
	db := database.GetManager().GetByCode(preview.CountryCode).WithContext(ctx)

	var notifType, notifTitle, notifMsg, notifURL string
	var authorID *uint

	switch normalizeContentType(preview.ContentType) {
	case "article":
		var item models.Article
		if err := db.Preload("Subject").Preload("Subject.SchoolClass").Preload("Semester").Preload("Semester.SchoolClass").First(&item, id).Error; err != nil {
			return nil, err
		}
		item.Title = utils.SanitizeInput(preview.FixedTitle)
		item.Content = utils.SanitizeHTML(preview.FixedContent)
		if err := db.Save(&item).Error; err != nil {
			return nil, err
		}
		authorID = item.AuthorID
		notifType = `App\Notifications\ArticleUpdatedByAI`
		notifTitle = fmt.Sprintf("تم تحديث المقالة: %s", shortNotificationTitle(item.Title, 70))
		notifMsg = fmt.Sprintf("تم اعتماد تحسين موثق بالأدلة وتحديث المقالة: %s", item.Title)
		notifURL = contentAuditEditURL("article", item.ID, preview.CountryCode)
	case "post":
		var item models.Post
		if err := db.Preload("Category").First(&item, id).Error; err != nil {
			return nil, err
		}
		item.Title = utils.SanitizeInput(preview.FixedTitle)
		item.Content = utils.StripBlockedLinks(utils.SanitizeHTML(preview.FixedContent))
		if err := db.Save(&item).Error; err != nil {
			return nil, err
		}
		authorID = item.AuthorID
		notifType = `App\Notifications\PostUpdatedByAI`
		notifTitle = fmt.Sprintf("تم تحديث المنشور: %s", shortNotificationTitle(item.Title, 70))
		notifMsg = fmt.Sprintf("تم اعتماد تحسين موثق بالأدلة وتحديث المنشور: %s", item.Title)
		notifURL = contentAuditEditURL("post", item.ID, preview.CountryCode)
	default:
		return nil, ErrUnsupportedContentType
	}

	now := time.Now()
	preview.Status = models.AIFixStatusApplied
	preview.AppliedByUserID = userID
	preview.AppliedAt = &now
	if err := s.repo.UpdateFixPreview(ctx, preview); err != nil {
		return nil, err
	}
	_ = s.repo.CreateApprovalLog(ctx, &models.ContentAIApprovalLog{FixPreviewID: preview.ID, DecisionID: preview.DecisionID, Action: models.AIFixStatusApplied, UserID: userID, Note: strings.TrimSpace(note)})

	if s.notification != nil {
		includeIDs := []uint{}
		if userID != nil {
			includeIDs = append(includeIDs, *userID)
		}
		if authorID != nil && (userID == nil || *authorID != *userID) {
			includeIDs = append(includeIDs, *authorID)
		}
		permissions := []string{"manage content audit", "manage articles", "manage posts"}
		go func() {
			_ = s.notification.NotifyUsersWithPermissions(notifType, notifTitle, notifMsg, notifURL, permissions, includeIDs...)
		}()
	}

	return preview, nil
}

func (s *Service) loadGroundedContent(ctx context.Context, decision *models.ContentAIDecision) (*groundedLoadedContent, error) {
	cc, _, id := normalizeContentReference(decision.ContentID, decision.CountryCode)
	if id == 0 {
		return nil, strconv.ErrSyntax
	}
	db := database.GetManager().GetByCode(cc).WithContext(ctx)
	switch normalizeContentType(decision.ContentType) {
	case "article":
		var item models.Article
		if err := db.Preload("Subject").Preload("Subject.SchoolClass").Preload("Semester").Preload("Semester.SchoolClass").Preload("Files").First(&item, id).Error; err != nil {
			return nil, err
		}
		gradeName, subjectName, semesterName := articleEducationContext(item)
		return &groundedLoadedContent{
			Type:              "article",
			ID:                item.ID,
			CountryCode:       cc,
			Title:             item.Title,
			Content:           item.Content,
			URL:               fmt.Sprintf("/%s/lesson/articles/%d", cc, item.ID),
			GradeName:         gradeName,
			SubjectName:       subjectName,
			SemesterName:      semesterName,
			CurriculumContext: buildCurriculumContext(cc, gradeName, subjectName, semesterName, ""),
			Files:             item.Files,
		}, nil
	case "post":
		var item models.Post
		if err := db.Preload("Category").Preload("Files").First(&item, id).Error; err != nil {
			return nil, err
		}
		categoryName := ""
		if item.Category != nil {
			categoryName = strings.TrimSpace(item.Category.Name)
		}
		return &groundedLoadedContent{
			Type:              "post",
			ID:                item.ID,
			CountryCode:       cc,
			Title:             item.Title,
			Content:           item.Content,
			URL:               fmt.Sprintf("/%s/posts/%d", cc, item.ID),
			CategoryName:      categoryName,
			CurriculumContext: buildCurriculumContext(cc, "", "", "", categoryName),
			Files:             item.Files,
		}, nil
	default:
		return nil, ErrUnsupportedContentType
	}
}

func buildGroundedSourcePack(content *groundedLoadedContent) groundedSourcePack {
	pack := groundedSourcePack{
		ContentType: content.Type,
		ContentID:   content.ID,
		CountryCode: content.CountryCode,
		Title:       strings.TrimSpace(content.Title),
		URL:         content.URL,
		Curriculum:  strings.TrimSpace(content.CurriculumContext),
	}
	if pack.Title != "" {
		pack.Evidence = append(pack.Evidence, groundedEvidence{ID: "content:title", Kind: "title", Label: "عنوان الصفحة", Text: pack.Title, Verified: true})
	}
	if body := truncateGroundingEvidence(normalizePlainText(content.Content)); body != "" {
		pack.Evidence = append(pack.Evidence, groundedEvidence{ID: "content:body", Kind: "body", Label: "النص الحالي", Text: body, Verified: true})
	}
	if pack.Curriculum != "" {
		pack.Evidence = append(pack.Evidence, groundedEvidence{ID: "content:curriculum", Kind: "curriculum", Label: "السياق الدراسي من قاعدة البيانات", Text: pack.Curriculum, Verified: true})
	}

	files := append([]models.File(nil), content.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].ID < files[j].ID })
	for _, file := range files {
		meta := attachmentMetadata(file)
		pack.Evidence = append(pack.Evidence, groundedEvidence{ID: fmt.Sprintf("attachment:%d:meta", file.ID), Kind: "attachment_metadata", Label: "بيانات المرفق " + firstNonEmptyLocal(file.FileName, filepath.Base(file.FilePath)), Text: meta, Verified: true})
		if extracted, ok := readAttachmentEvidence(file); ok {
			pack.Evidence = append(pack.Evidence, groundedEvidence{ID: fmt.Sprintf("attachment:%d:text", file.ID), Kind: "attachment_text", Label: "نص مستخرج من المرفق " + firstNonEmptyLocal(file.FileName, filepath.Base(file.FilePath)), Text: truncateGroundingEvidence(extracted), Verified: true})
		}
	}
	return pack
}

func attachmentMetadata(file models.File) string {
	parts := []string{}
	if name := strings.TrimSpace(file.FileName); name != "" {
		parts = append(parts, "اسم الملف: "+name)
	}
	if t := strings.TrimSpace(file.FileType); t != "" {
		parts = append(parts, "نوع الملف: "+t)
	}
	if file.FileCategory != nil && strings.TrimSpace(*file.FileCategory) != "" {
		parts = append(parts, "تصنيف الملف: "+strings.TrimSpace(*file.FileCategory))
	}
	if mime := strings.TrimSpace(file.MimeType); mime != "" {
		parts = append(parts, "MIME: "+mime)
	}
	if file.FileSize > 0 {
		parts = append(parts, fmt.Sprintf("الحجم: %d بايت", file.FileSize))
	}
	if len(parts) == 0 {
		parts = append(parts, "يوجد مرفق مرتبط بهذه الصفحة")
	}
	return strings.Join(parts, " | ")
}

func readAttachmentEvidence(file models.File) (string, bool) {
	path, ok := resolveAttachmentPath(file.FilePath)
	if !ok {
		return "", false
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt", ".md", ".csv", ".json", ".html", ".htm", ".xml":
		f, err := os.Open(path)
		if err != nil {
			return "", false
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, groundingReadLimit))
		if err != nil {
			return "", false
		}
		text := string(data)
		if ext == ".html" || ext == ".htm" || ext == ".xml" {
			text = normalizePlainText(text)
		}
		return strings.TrimSpace(text), strings.TrimSpace(text) != ""
	case ".docx":
		text, err := extractDOCXText(path)
		return text, err == nil && strings.TrimSpace(text) != ""
	case ".pdf":
		text, err := extractPDFText(path)
		return text, err == nil && strings.TrimSpace(text) != ""
	default:
		return "", false
	}
}

func resolveAttachmentPath(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "://") {
		return "", false
	}
	clean := filepath.Clean(raw)
	candidates := []string{}
	if filepath.IsAbs(clean) {
		candidates = append(candidates, clean)
	}
	trimmed := strings.TrimLeft(clean, "/\\")
	for _, root := range []string{
		strings.TrimSpace(os.Getenv("IMANJO_STORAGE_ROOT")),
		strings.TrimSpace(os.Getenv("STORAGE_ROOT")),
		"/var/www/vhosts/imanjo.com/api.imanjo.com/storage",
		"/var/www/vhosts/imanjo.com/httpdocs/storage",
		"/var/www/vhosts/imanjo.com/httpdocs/public/storage",
	} {
		if root == "" {
			continue
		}
		candidates = append(candidates, filepath.Join(root, strings.TrimPrefix(trimmed, "storage/")))
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func extractDOCXText(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(io.LimitReader(r, groundingReadLimit))
		r.Close()
		if err != nil {
			return "", err
		}
		text := strings.ReplaceAll(string(data), "</w:p>", "\n")
		text = normalizePlainText(text)
		return strings.TrimSpace(html.UnescapeString(text)), nil
	}
	return "", errors.New("docx document.xml not found")
}

func extractPDFText(path string) (string, error) {
	binary, err := exec.LookPath("pdftotext")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-layout", path, "-")
	var out bytes.Buffer
	cmd.Stdout = &limitedWriter{W: &out, N: groundingReadLimit}
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

type limitedWriter struct {
	W io.Writer
	N int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.N <= 0 {
		return len(p), nil
	}
	toWrite := p
	if int64(len(toWrite)) > w.N {
		toWrite = toWrite[:w.N]
	}
	n, err := w.W.Write(toWrite)
	w.N -= int64(n)
	if err != nil {
		return n, err
	}
	return len(p), nil
}

func truncateGroundingEvidence(value string) string {
	value = strings.TrimSpace(value)
	if len([]rune(value)) <= groundingEvidenceLimit {
		return value
	}
	return string([]rune(value)[:groundingEvidenceLimit]) + "…"
}

func sanitizeGroundedFacts(in groundedFactExtraction, pack groundedSourcePack) groundedFactExtraction {
	validEvidence := map[string]bool{}
	for _, evidence := range pack.Evidence {
		validEvidence[evidence.ID] = evidence.Verified
	}
	cleanFacts := make([]groundedFact, 0, len(in.Facts))
	for _, fact := range in.Facts {
		fact.Claim = strings.TrimSpace(fact.Claim)
		if fact.Claim == "" {
			continue
		}
		ids := []string{}
		seen := map[string]bool{}
		for _, id := range fact.EvidenceIDs {
			id = strings.TrimSpace(id)
			if id != "" && validEvidence[id] && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			continue
		}
		fact.EvidenceIDs = ids
		if fact.Confidence < 0 {
			fact.Confidence = 0
		}
		if fact.Confidence > 100 {
			fact.Confidence = 100
		}
		cleanFacts = append(cleanFacts, fact)
	}
	in.Facts = cleanFacts
	in.Audience = compactStrings(in.Audience)
	in.SourceNotes = compactStrings(in.SourceNotes)
	if len(in.Facts) == 0 {
		in.InsufficientSource = true
	}
	return in
}

func validUsedFactIndexes(indexes []int, factCount int) bool {
	if factCount <= 0 || len(indexes) == 0 {
		return false
	}
	for _, index := range indexes {
		if index < 0 || index >= factCount {
			return false
		}
	}
	return true
}

func compactStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func runGroundedFactExtraction(ctx context.Context, pack groundedSourcePack) (groundedFactExtraction, string, error) {
	var out groundedFactExtraction
	packJSON, _ := json.Marshal(pack)
	system := `أنت مدقق مصادر لمحتوى تعليمي. لا تكتب المقال. استخرج فقط حقائق صريحة يمكن إثباتها من الأدلة المقدمة. كل حقيقة يجب أن تشير إلى evidence_ids صحيحة. ممنوع الاستنتاج من عنوان الصفحة وحده أن المرفق يحتوي أهدافًا أو أنشطة أو أسئلة أو حلولًا ما لم يذكر الدليل ذلك. إذا لم تكف الأدلة لإنشاء صفحة مفيدة ودقيقة، اجعل insufficient_source=true. أعد JSON فقط.`
	user := fmt.Sprintf(`نسخة البرومبت: %s\nحزمة المصدر:\n%s\n\nأرجع JSON بالمفاتيح: purpose, audience, facts, insufficient_source, source_notes. facts عنصره: claim, evidence_ids, confidence.`, groundingPromptVersion, string(packJSON))
	model, err := groundedAIJSON(ctx, "fact_extractor", system, user, &out)
	return out, model, err
}

func runGroundedWriter(ctx context.Context, pack groundedSourcePack, facts groundedFactExtraction) (groundedDraft, string, error) {
	var out groundedDraft
	factsJSON, _ := json.Marshal(facts)
	contextJSON, _ := json.Marshal(map[string]interface{}{
		"title":        pack.Title,
		"content_type": pack.ContentType,
		"country_code": pack.CountryCode,
		"curriculum":   pack.Curriculum,
	})
	system := `أنت محرر تعليمي عربي. اكتب مسودة مفيدة ودقيقة اعتمادًا حصريًا على الحقائق المستخرجة. لا تضف أي معلومة غير موجودة في facts. لا تطارد عدد كلمات ولا تضف حشو SEO. إذا كانت الأدلة محدودة فاكتب صفحة قصيرة صادقة بدل اختراع تفاصيل. لا تقل إن الملف يحتوي أهدافًا أو أنشطة أو أسئلة أو حلولًا إلا إذا كانت حقيقة صريحة. اجعل الملف جزءًا من المحتوى لا مجرد غلاف له: اشرح ما هو المورد، لمن يفيد، وما الذي يمكن إثباته عنه، ثم وجّه القارئ للاستفادة من المرفق. HTML نظيف فقط داخل content_html بعناوين h2 وفقرات p عند الحاجة. أعد JSON فقط.`
	user := fmt.Sprintf(`السياق:\n%s\n\nالحقائق المسموح استخدامها فقط:\n%s\n\nأرجع JSON بالمفاتيح: title, content_html, used_fact_indexes. used_fact_indexes هي فهارس الحقائق المستخدمة (تبدأ من 0).`, string(contextJSON), string(factsJSON))
	model, err := groundedAIJSON(ctx, "grounded_writer", system, user, &out)
	return out, model, err
}

func runGroundedValidator(ctx context.Context, pack groundedSourcePack, facts groundedFactExtraction, draft groundedDraft) (groundedValidation, string, error) {
	var out groundedValidation
	payload, _ := json.Marshal(map[string]interface{}{"source_pack": pack, "facts": facts, "draft": draft})
	system := `أنت مدقق ادعاءات صارم. قارن كل ادعاء واقعي في المسودة مع حزمة الأدلة والحقائق. اعتبر أي معلومة غير مثبتة unsupported حتى لو بدت منطقية تربويًا. لا تكافئ طول النص. grounding_score يقيس نسبة الادعاءات المدعومة ودقتها من 0 إلى 100. إذا وجد ادعاء مهم غير مدعوم فاذكره حرفيًا أو باختصار في unsupported_claims. أعد JSON فقط.`
	user := fmt.Sprintf(`راجع هذه البيانات:\n%s\n\nأرجع JSON بالمفاتيح: grounding_score, supported_claims, unsupported_claims, notes.`, string(payload))
	model, err := groundedAIJSON(ctx, "claim_validator", system, user, &out)
	return out, model, err
}

func groundedAIJSON(ctx context.Context, stage, systemPrompt, userPrompt string, out interface{}) (string, error) {
	apiKey := firstNonEmptyLocal(os.Getenv("TOGETHER_AI_API_KEY"), os.Getenv("TOGETHER_AI_KEY"), os.Getenv("TOGETHER_API_KEY"))
	if apiKey == "" {
		return "", ErrGroundedAIUnavailable
	}
	baseURL := strings.TrimRight(firstNonEmptyLocal(os.Getenv("TOGETHER_AI_BASE_URL"), "https://api.together.ai/v1"), "/")
	models := groundedModelCandidates()
	if len(models) == 0 {
		return "", ErrGroundedAIUnavailable
	}

	var lastErr error
	for _, model := range models {
		payload := map[string]interface{}{
			"model": model,
			"messages": []map[string]string{
				{"role": "system", "content": systemPrompt},
				{"role": "user", "content": userPrompt},
			},
			"temperature": 0.12,
			"top_p":       0.85,
			"max_tokens":  2600,
			"response_format": map[string]interface{}{"type": "json_object"},
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		requestCtx, cancel := context.WithTimeout(ctx, 65*time.Second)
		req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			cancel()
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
		resp.Body.Close()
		cancel()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("grounded AI %s failed with HTTP %d", stage, resp.StatusCode)
			continue
		}
		var completion struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(responseBody, &completion); err != nil || len(completion.Choices) == 0 {
			if err == nil {
				err = errors.New("grounded AI response has no choices")
			}
			lastErr = err
			continue
		}
		raw := cleanGroundedJSON(completion.Choices[0].Message.Content)
		if err := json.Unmarshal([]byte(raw), out); err != nil {
			lastErr = fmt.Errorf("grounded AI %s returned invalid JSON: %w", stage, err)
			continue
		}
		return model, nil
	}
	if lastErr == nil {
		lastErr = ErrGroundedAIUnavailable
	}
	return "", fmt.Errorf("%w: %v", ErrGroundedAIUnavailable, lastErr)
}

func groundedModelCandidates() []string {
	values := []string{}
	for _, raw := range []string{os.Getenv("AI_MODELS_FIX_FINAL"), os.Getenv("AI_MODELS_FIX_QUALITY"), os.Getenv("TOGETHER_AI_MODEL"), "deepseek-ai/DeepSeek-V4-Pro"} {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				values = append(values, part)
			}
		}
	}
	return compactStrings(values)
}

func cleanGroundedJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end > start {
			raw = raw[start : end+1]
		}
	}
	return raw
}

func buildGroundingSummary(validation groundedValidation, evidenceCount int, model string, facts groundedFactExtraction, pack groundedSourcePack) string {
	status := "grounded"
	labels := []string{}
	for _, evidence := range pack.Evidence {
		labels = append(labels, evidence.Label)
	}
	if len(labels) > 5 {
		labels = labels[:5]
	}
	return fmt.Sprintf("%s status=%s score=%d evidence=%d unsupported=%d model=%s prompt=%s\nتم إنشاء المسودة من أدلة المصدر فقط، ثم فحص الادعاءات بشكل مستقل. الغرض المستخلص: %s. مصادر بارزة: %s. لا يوجد حد كلمات إجباري؛ الأولوية للدقة والفائدة.", groundingSummaryPrefix, status, validation.GroundingScore, evidenceCount, len(validation.UnsupportedClaims), safeGroundingToken(model), groundingPromptVersion, strings.TrimSpace(facts.Purpose), strings.Join(labels, "، "))
}

var groundingSummaryPattern = regexp.MustCompile(`^\[GROUNDING_V2\]\s+status=([^\s]+)\s+score=(\d+)\s+evidence=(\d+)\s+unsupported=(\d+)\s+model=([^\s]+)`) 

func parseGroundingSummary(summary string) (groundingSummaryMeta, bool) {
	matches := groundingSummaryPattern.FindStringSubmatch(strings.TrimSpace(summary))
	if len(matches) != 6 {
		return groundingSummaryMeta{}, false
	}
	score, err1 := strconv.Atoi(matches[2])
	evidence, err2 := strconv.Atoi(matches[3])
	unsupported, err3 := strconv.Atoi(matches[4])
	if err1 != nil || err2 != nil || err3 != nil {
		return groundingSummaryMeta{}, false
	}
	return groundingSummaryMeta{Status: matches[1], Score: score, Evidence: evidence, Unsupported: unsupported, Model: matches[5]}, true
}

func safeGroundingToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.ReplaceAll(value, "\n", "_")
	return value
}
