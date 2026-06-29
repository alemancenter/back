package services

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/models"
)

// TeacherWatermarkedFile describes the file that should be sent to the user.
type TeacherWatermarkedFile struct {
	Path    string
	Name    string
	Mime    string
	Applied bool
	Text    string
}

// BuildTeacherWatermarkText creates the license line used in Teacher Pro files.
// It intentionally avoids exposing the full email address.
func BuildTeacherWatermarkTextForUser(user *models.User, downloadCode string) string {
	name := "Teacher Pro"
	email := ""
	userID := uint(0)
	if user != nil {
		userID = user.ID
		name = strings.TrimSpace(user.Name)
		email = maskEmail(user.Email)
	}
	if name == "" {
		name = fmt.Sprintf("User %d", userID)
	}

	return fmt.Sprintf("Alemancenter Teacher Pro | مرخص للمعلم: %s | البريد: %s | الرمز: %s | التاريخ: %s",
		name,
		email,
		downloadCode,
		time.Now().Format("2006-01-02"),
	)
}

// PrepareTeacherPremiumDownloadFile returns a watermarked PDF path when possible.
// Non-PDF files remain protected through access checks and download logs, but are sent unchanged.
func PrepareTeacherPremiumDownloadFile(user *models.User, file *models.TeacherPremiumFile, download *models.TeacherPremiumDownload) (*TeacherWatermarkedFile, error) {
	if file == nil || download == nil {
		return nil, ErrTeacherPlanNotFound
	}

	originalPath := strings.TrimSpace(file.PrivatePath)
	result := &TeacherWatermarkedFile{
		Path: originalPath,
		Name: file.OriginalFilename,
		Mime: file.MimeType,
		Text: BuildTeacherWatermarkTextForUser(user, download.DownloadCode),
	}

	if originalPath == "" {
		return result, nil
	}

	if !file.WatermarkEnabled || !isPDFFile(file) {
		return result, nil
	}

	outDir := filepath.Join("storage", "private", "teacher-watermarked", fmt.Sprintf("user-%d", download.UserID), fmt.Sprintf("file-%d", file.ID))
	if err := os.MkdirAll(outDir, 0750); err != nil {
		return result, err
	}

	base := strings.TrimSuffix(safeFilename(file.OriginalFilename), filepath.Ext(file.OriginalFilename))
	if base == "" {
		base = fmt.Sprintf("premium-file-%d", file.ID)
	}
	outPath := filepath.Join(outDir, fmt.Sprintf("%s-%s.pdf", base, safeFilename(download.DownloadCode)))

	if err := addPDFAnnotationWatermark(originalPath, outPath, result.Text); err != nil {
		// If an old/complex PDF cannot be incrementally stamped, do not block the teacher.
		// The protected access check and download log still apply.
		return result, nil
	}

	result.Path = outPath
	result.Name = base + "-Teacher-Pro.pdf"
	result.Mime = "application/pdf"
	result.Applied = true
	return result, nil
}

func isPDFFile(file *models.TeacherPremiumFile) bool {
	mime := strings.ToLower(strings.TrimSpace(file.MimeType))
	name := strings.ToLower(strings.TrimSpace(file.OriginalFilename))
	fileType := strings.ToLower(strings.TrimSpace(file.FileType))
	return strings.Contains(mime, "pdf") || strings.HasSuffix(name, ".pdf") || fileType == "pdf"
}

func maskEmail(email string) string {
	email = strings.TrimSpace(email)
	if email == "" || !strings.Contains(email, "@") {
		return ""
	}
	parts := strings.SplitN(email, "@", 2)
	local := parts[0]
	domain := parts[1]
	if len(local) <= 2 {
		return local[:1] + "***@" + domain
	}
	return local[:2] + "***@" + domain
}

func safeFilename(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "file"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-", " ", "-")
	value = replacer.Replace(value)
	value = regexp.MustCompile(`[^A-Za-z0-9._\-]+`).ReplaceAllString(value, "-")
	value = strings.Trim(value, ".-")
	if value == "" {
		return "file"
	}
	if len(value) > 160 {
		value = value[:160]
	}
	return value
}

func pdfEscape(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `(`, `\(`)
	value = strings.ReplaceAll(value, `)`, `\)`)
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

type pdfObj struct {
	num     int
	gen     int
	start   int
	end     int
	content string
}

func addPDFAnnotationWatermark(inPath, outPath, text string) error {
	data, err := os.ReadFile(inPath)
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("%PDF-")) {
		return fmt.Errorf("not a pdf")
	}

	objects := parsePDFObjects(data)
	if len(objects) == 0 {
		return fmt.Errorf("no pdf objects found")
	}

	pageObjects := make([]pdfObj, 0)
	maxObj := 0
	for _, obj := range objects {
		if obj.num > maxObj {
			maxObj = obj.num
		}
		if isPDFPageObject(obj.content) {
			pageObjects = append(pageObjects, obj)
		}
	}
	if len(pageObjects) == 0 {
		return fmt.Errorf("no page objects found")
	}

	rootRef := findTrailerRoot(data)
	infoRef := findTrailerInfo(data)
	prevXref := findLastStartXref(data)
	if rootRef == "" || prevXref < 0 {
		return fmt.Errorf("invalid trailer")
	}

	var out bytes.Buffer
	out.Write(data)
	if len(data) == 0 || data[len(data)-1] != '\n' {
		out.WriteByte('\n')
	}

	xrefEntries := make(map[int]int)
	nextObj := maxObj + 1
	escaped := pdfEscape(text)

	for _, page := range pageObjects {
		annotObjNum := nextObj
		nextObj++

		annotOffset := out.Len()
		xrefEntries[annotObjNum] = annotOffset
		out.WriteString(fmt.Sprintf("%d 0 obj\n", annotObjNum))
		out.WriteString(fmt.Sprintf("<< /Type /Annot /Subtype /FreeText /Rect [36 36 559 92] /Contents (%s) /DA (/Helvetica 10 Tf 0.55 g) /Q 1 /F 4 /Border [0 0 0] /C [0.92 0.92 0.92] >>\n", escaped))
		out.WriteString("endobj\n")

		updatedPage := addAnnotationToPageObject(page.content, annotObjNum)
		pageOffset := out.Len()
		xrefEntries[page.num] = pageOffset
		out.WriteString(fmt.Sprintf("%d %d obj\n", page.num, page.gen))
		out.WriteString(updatedPage)
		if !strings.HasSuffix(updatedPage, "\n") {
			out.WriteByte('\n')
		}
		out.WriteString("endobj\n")
	}

	xrefOffset := out.Len()
	size := nextObj
	writeIncrementalXref(&out, xrefEntries, size, rootRef, infoRef, prevXref)

	out.WriteString("startxref\n")
	out.WriteString(strconv.Itoa(xrefOffset))
	out.WriteString("\n%%EOF\n")

	return os.WriteFile(outPath, out.Bytes(), 0640)
}

func parsePDFObjects(data []byte) []pdfObj {
	re := regexp.MustCompile(`(?s)(\d+)\s+(\d+)\s+obj(.*?)endobj`)
	matches := re.FindAllSubmatchIndex(data, -1)
	objects := make([]pdfObj, 0, len(matches))
	for _, m := range matches {
		num, _ := strconv.Atoi(string(data[m[2]:m[3]]))
		gen, _ := strconv.Atoi(string(data[m[4]:m[5]]))
		objects = append(objects, pdfObj{
			num:     num,
			gen:     gen,
			start:   m[0],
			end:     m[1],
			content: strings.TrimSpace(string(data[m[6]:m[7]])),
		})
	}
	return objects
}

func isPDFPageObject(content string) bool {
	normalized := regexp.MustCompile(`\s+`).ReplaceAllString(content, " ")
	return strings.Contains(normalized, "/Type /Page") && !strings.Contains(normalized, "/Type /Pages")
}

func addAnnotationToPageObject(content string, annotObjNum int) string {
	ref := fmt.Sprintf("%d 0 R", annotObjNum)
	annotsArray := regexp.MustCompile(`(?s)/Annots\s*\[(.*?)\]`)
	if annotsArray.MatchString(content) {
		return annotsArray.ReplaceAllString(content, "/Annots [$1 "+ref+"]")
	}

	annotsRef := regexp.MustCompile(`/Annots\s+(\d+\s+\d+\s+R)`)
	if annotsRef.MatchString(content) {
		return annotsRef.ReplaceAllString(content, "/Annots [$1 "+ref+"]")
	}

	idx := strings.LastIndex(content, ">>")
	if idx == -1 {
		return content
	}
	return content[:idx] + fmt.Sprintf(" /Annots [%s] ", ref) + content[idx:]
}

func findTrailerRoot(data []byte) string {
	re := regexp.MustCompile(`(?s)trailer\s*<<.*?/Root\s+(\d+\s+\d+\s+R).*?>>`)
	matches := re.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return ""
	}
	return string(matches[len(matches)-1][1])
}

func findTrailerInfo(data []byte) string {
	re := regexp.MustCompile(`(?s)trailer\s*<<.*?/Info\s+(\d+\s+\d+\s+R).*?>>`)
	matches := re.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return ""
	}
	return string(matches[len(matches)-1][1])
}

func findLastStartXref(data []byte) int {
	re := regexp.MustCompile(`startxref\s+(\d+)`)
	matches := re.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return -1
	}
	value, err := strconv.Atoi(string(matches[len(matches)-1][1]))
	if err != nil {
		return -1
	}
	return value
}

func writeIncrementalXref(out *bytes.Buffer, entries map[int]int, size int, rootRef string, infoRef string, prev int) {
	keys := make([]int, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	// tiny insertion sort to avoid adding a dependency.
	for i := 1; i < len(keys); i++ {
		j := i
		for j > 0 && keys[j-1] > keys[j] {
			keys[j-1], keys[j] = keys[j], keys[j-1]
			j--
		}
	}

	out.WriteString("xref\n")
	for _, objNum := range keys {
		out.WriteString(fmt.Sprintf("%d 1\n", objNum))
		out.WriteString(fmt.Sprintf("%010d 00000 n \n", entries[objNum]))
	}

	out.WriteString("trailer\n")
	out.WriteString(fmt.Sprintf("<< /Size %d /Root %s", size, rootRef))
	if infoRef != "" {
		out.WriteString(fmt.Sprintf(" /Info %s", infoRef))
	}
	out.WriteString(fmt.Sprintf(" /Prev %d >>\n", prev))
}
