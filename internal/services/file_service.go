package services

import (
	"archive/zip"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"github.com/gabriel-vasile/mimetype"
)

type UploadResponse struct {
	Path string `json:"path"`
	URL  string `json:"url"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type"`
}

// FileService handles file operations like uploading, path mapping, and size calculations.
type FileService struct {
	cfg  config.StorageConfig
	repo repositories.FileRepository
}

// UploadedFile represents a successfully uploaded file
type UploadedFile struct {
	Path     string
	URL      string
	Name     string
	Size     int64
	MimeType string
	Ext      string
}

// AllowedImageTypes lists accepted image MIME types
var AllowedImageTypes = []string{
	"image/jpeg", "image/png", "image/gif", "image/webp", "image/svg+xml",
	"image/x-icon", "image/vnd.microsoft.icon",
}

// AllowedDocumentTypes lists accepted document MIME types
var AllowedDocumentTypes = []string{
	"application/pdf",
	"application/rtf",
	"application/msword",
	"application/vnd.ms-word",
	"application/vnd.ms-word.document.macroEnabled.12",
	"application/vnd.ms-word.template.macroEnabled.12",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.template",
	"application/vnd.ms-excel",
	"application/msexcel",
	"application/vnd.ms-excel.sheet.binary.macroEnabled.12",
	"application/vnd.ms-excel.sheet.macroEnabled.12",
	"application/vnd.ms-excel.template.macroEnabled.12",
	"application/vnd.ms-excel.addin.macroEnabled.12",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.template",
	"application/vnd.ms-powerpoint",
	"application/mspowerpoint",
	"application/vnd.ms-powerpoint.presentation.macroEnabled.12",
	"application/vnd.ms-powerpoint.template.macroEnabled.12",
	"application/vnd.ms-powerpoint.slideshow.macroEnabled.12",
	"application/vnd.ms-powerpoint.addin.macroEnabled.12",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation",
	"application/vnd.openxmlformats-officedocument.presentationml.template",
	"application/vnd.openxmlformats-officedocument.presentationml.slideshow",
	"application/vnd.oasis.opendocument.text",
	"application/vnd.oasis.opendocument.text-template",
	"application/vnd.oasis.opendocument.spreadsheet",
	"application/vnd.oasis.opendocument.spreadsheet-template",
	"application/vnd.oasis.opendocument.presentation",
	"application/vnd.oasis.opendocument.presentation-template",
	"application/x-ole-storage",
	"application/zip",
	"application/csv",
	"text/csv",
	"text/plain",
	"text/rtf",
}

// allowedImageExts maps MIME type → allowed file extensions for images.
// This prevents polyglot files that pass MIME detection but carry a dangerous extension.
var allowedImageExts = map[string][]string{
	"image/jpeg":               {".jpg", ".jpeg"},
	"image/png":                {".png"},
	"image/gif":                {".gif"},
	"image/webp":               {".webp"},
	"image/svg+xml":            {".svg"},
	"image/x-icon":             {".ico"},
	"image/vnd.microsoft.icon": {".ico"},
}

// allowedDocumentExts maps MIME type → allowed file extensions for documents.
var allowedDocumentExts = map[string][]string{
	"application/pdf":         {".pdf"},
	"application/rtf":         {".rtf"},
	"application/msword":      {".doc", ".dot"},
	"application/vnd.ms-word": {".doc", ".dot"},
	"application/vnd.ms-word.template.macroEnabled.12":                          {".dotm"},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   {".docx"},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.template":   {".dotx"},
	"application/vnd.ms-excel":                                                  {".xls", ".xlt"},
	"application/msexcel":                                                       {".xls", ".xlt"},
	"application/vnd.ms-excel.sheet.binary.macroEnabled.12":                     {".xlsb"},
	"application/vnd.ms-excel.template.macroEnabled.12":                         {".xltm"},
	"application/vnd.ms-excel.addin.macroEnabled.12":                            {".xlam"},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         {".xlsx"},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.template":      {".xltx"},
	"application/vnd.ms-powerpoint":                                             {".ppt"},
	"application/mspowerpoint":                                                  {".ppt"},
	"application/vnd.ms-powerpoint.template.macroEnabled.12":                    {".potm"},
	"application/vnd.ms-powerpoint.slideshow.macroEnabled.12":                   {".ppsm"},
	"application/vnd.ms-powerpoint.addin.macroEnabled.12":                       {".ppam"},
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": {".pptx"},
	"application/vnd.openxmlformats-officedocument.presentationml.template":     {".potx"},
	"application/vnd.openxmlformats-officedocument.presentationml.slideshow":    {".ppsx"},
	"application/vnd.oasis.opendocument.text":                                   {".odt"},
	"application/vnd.oasis.opendocument.text-template":                          {".ott"},
	"application/vnd.oasis.opendocument.spreadsheet":                            {".ods"},
	"application/vnd.oasis.opendocument.spreadsheet-template":                   {".ots"},
	"application/vnd.oasis.opendocument.presentation":                           {".odp"},
	"application/vnd.oasis.opendocument.presentation-template":                  {".otp"},
	"application/x-ole-storage":                                                 {".doc", ".dot", ".xls", ".xlt", ".ppt", ".pps"},
	"application/zip":                                                           {".zip"},
	"application/csv":                                                           {".csv"},
	"text/csv":                                                                  {".csv"},
	"text/plain":                                                                {".txt", ".csv"},
	"text/rtf":                                                                  {".rtf"},
}

var officeZipPrefixesByExt = map[string]string{
	".docx": "word/",
	".dotx": "word/",
	".dotm": "word/",
	".xlsx": "xl/",
	".xlsb": "xl/",
	".xltx": "xl/",
	".xltm": "xl/",
	".xlam": "xl/",
	".pptx": "ppt/",
	".potx": "ppt/",
	".potm": "ppt/",
	".ppsx": "ppt/",
	".ppsm": "ppt/",
	".ppam": "ppt/",
}

var officeMimeByExt = map[string]string{
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".dotx": "application/vnd.openxmlformats-officedocument.wordprocessingml.template",
	".dotm": "application/vnd.ms-word.template.macroEnabled.12",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".xlsb": "application/vnd.ms-excel.sheet.binary.macroEnabled.12",
	".xltx": "application/vnd.openxmlformats-officedocument.spreadsheetml.template",
	".xltm": "application/vnd.ms-excel.template.macroEnabled.12",
	".xlam": "application/vnd.ms-excel.addin.macroEnabled.12",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".potx": "application/vnd.openxmlformats-officedocument.presentationml.template",
	".potm": "application/vnd.ms-powerpoint.template.macroEnabled.12",
	".ppsx": "application/vnd.openxmlformats-officedocument.presentationml.slideshow",
	".ppsm": "application/vnd.ms-powerpoint.slideshow.macroEnabled.12",
	".ppam": "application/vnd.ms-powerpoint.addin.macroEnabled.12",
}

// MaxImageSize is 10MB
const MaxImageSize = 10 * 1024 * 1024

// MaxDocumentSize is 50MB
const MaxDocumentSize = 50 * 1024 * 1024

// NewFileService creates a new FileService
func NewFileService(repo repositories.FileRepository) *FileService {
	return &FileService{
		cfg:  config.Get().Storage,
		repo: repo,
	}
}

// UploadImage validates and saves an image file
func (s *FileService) UploadImage(header *multipart.FileHeader, subdir string) (*UploadedFile, error) {
	if header.Size > MaxImageSize {
		return nil, fmt.Errorf("حجم الصورة يتجاوز الحد المسموح (10MB)")
	}

	return s.upload(header, subdir, AllowedImageTypes)
}

// UploadDocument validates and saves a document file
func (s *FileService) UploadDocument(header *multipart.FileHeader, subdir string) (*UploadedFile, error) {
	if header.Size > MaxDocumentSize {
		return nil, fmt.Errorf("حجم الملف يتجاوز الحد المسموح (50MB)")
	}

	return s.upload(header, subdir, AllowedDocumentTypes)
}

// upload is the core upload implementation
func (s *FileService) upload(header *multipart.FileHeader, subdir string, allowed []string) (*UploadedFile, error) {
	src, err := header.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open uploaded file: %w", MapError(err))
	}
	defer src.Close()

	mtype, err := mimetype.DetectReader(src)
	if err != nil {
		return nil, fmt.Errorf("failed to detect file type: %w", MapError(err))
	}

	uploadedExt := strings.ToLower(filepath.Ext(header.Filename))
	detectedBaseMime := strings.Split(mtype.String(), ";")[0]
	baseMime := normalizeOfficeZipMime(src, header.Size, detectedBaseMime, uploadedExt)
	mimeType := mtype.String()
	if baseMime != detectedBaseMime {
		mimeType = baseMime
	}

	// Validate MIME type
	if !isAllowedMime(baseMime, allowed) {
		return nil, fmt.Errorf("نوع الملف غير مسموح: %s", baseMime)
	}

	// Validate that the uploaded filename's extension matches the detected MIME type.
	// This blocks polyglot files (e.g. a PHP script with a .jpg extension that passes
	// MIME detection but would execute if served directly).
	if uploadedExt != "" {
		allowedExts := allowedExtensionsForMime(baseMime)
		if len(allowedExts) > 0 && !containsStr(allowedExts, uploadedExt) {
			return nil, fmt.Errorf("امتداد الملف غير مطابق لنوعه: %s", uploadedExt)
		}
	}

	// Reset reader
	if _, err := src.(io.Seeker).Seek(0, 0); err != nil {
		return nil, MapError(err)
	}

	// Preserve the uploaded extension only after it has passed the MIME allow-list check.
	ext := uploadedExt
	if ext == "" {
		ext = defaultExtensionForMime(baseMime)
	}
	if ext == "" {
		ext = mtype.Extension()
	}
	filename := randomAlphanumeric(12) + ext

	// relPath is always forward-slash (URL-safe); absPath uses OS separators for disk I/O
	relPath := path.Join(subdir, filename)
	absPath := filepath.Join(s.cfg.Path, filepath.FromSlash(relPath))

	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create upload directory: %w", MapError(err))
	}

	dst, err := os.Create(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", MapError(err))
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return nil, fmt.Errorf("failed to save file: %w", MapError(err))
	}

	return &UploadedFile{
		Path:     relPath,
		URL:      s.cfg.URL + "/" + relPath,
		Name:     header.Filename,
		Size:     header.Size,
		MimeType: mimeType,
		Ext:      ext,
	}, nil
}

// Delete removes a file from storage
func (s *FileService) Delete(relPath string) error {
	absPath := filepath.Join(s.cfg.Path, normalizeLegacyPath(relPath))
	if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
		return MapError(err)
	}
	return nil
}

// GetAbsPath returns the absolute path for a relative storage path
func (s *FileService) GetAbsPath(relPath string) string {
	return filepath.Join(s.cfg.Path, normalizeLegacyPath(relPath))
}

// SafeGetAbsPath resolves relPath within the storage root and rejects any
// path that escapes it (path traversal). Returns an error for invalid paths.
func (s *FileService) SafeGetAbsPath(relPath string) (string, error) {
	// Strip legacy "storage/" prefix that old Laravel records may have in the DB
	relPath = normalizeLegacyPath(relPath)
	// Clean removes ".." and "." components
	cleaned := filepath.Clean(relPath)
	// Reject absolute paths supplied by the caller
	if filepath.IsAbs(cleaned) {
		return "", errors.New("absolute paths are not allowed")
	}
	storageRoot := filepath.Clean(s.cfg.Path)
	abs := filepath.Join(storageRoot, cleaned)
	// Ensure the resolved path is still under the storage root
	if !strings.HasPrefix(abs, storageRoot+string(filepath.Separator)) {
		return "", errors.New("path traversal detected")
	}
	return abs, nil
}

// normalizeLegacyPath strips the leading "storage/" prefix that old Laravel
// records may have persisted in the database (e.g. "storage/files/foo.pdf" → "files/foo.pdf").
func normalizeLegacyPath(relPath string) string {
	return strings.TrimPrefix(relPath, "storage/")
}

// randomAlphanumeric generates a cryptographically random lowercase alphanumeric
// string of length n — matches the format produced by Laravel's Str::random().
func randomAlphanumeric(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

func isAllowedMime(mtype string, allowed []string) bool {
	for _, a := range allowed {
		if a == mtype {
			return true
		}
	}
	return false
}

func allowedExtensionsForMime(mtype string) []string {
	if exts, ok := allowedImageExts[mtype]; ok {
		return exts
	}
	if exts, ok := allowedDocumentExts[mtype]; ok {
		return exts
	}
	return nil
}

func defaultExtensionForMime(mtype string) string {
	exts := allowedExtensionsForMime(mtype)
	if len(exts) == 0 {
		return ""
	}
	return exts[0]
}

func normalizeOfficeZipMime(file multipart.File, size int64, baseMime, uploadedExt string) string {
	expectedPrefix, ok := officeZipPrefixesByExt[uploadedExt]
	if !ok || !isOfficeZipCandidateMime(baseMime) {
		return baseMime
	}
	if !zipContainsOfficeEntries(file, size, expectedPrefix) {
		return baseMime
	}
	if officeMime, ok := officeMimeByExt[uploadedExt]; ok {
		return officeMime
	}
	return baseMime
}

func isOfficeZipCandidateMime(mtype string) bool {
	return mtype == "application/zip" ||
		strings.Contains(mtype, "officedocument") ||
		strings.Contains(mtype, "macroEnabled")
}

func zipContainsOfficeEntries(file multipart.File, size int64, expectedPrefix string) bool {
	if size <= 0 {
		return false
	}

	zr, err := zip.NewReader(file, size)
	if err != nil {
		return false
	}

	hasContentTypes := false
	hasExpectedOfficeDir := false
	for _, zipFile := range zr.File {
		name := strings.TrimPrefix(strings.ToLower(zipFile.Name), "/")
		if name == "[content_types].xml" {
			hasContentTypes = true
		}
		if strings.HasPrefix(name, expectedPrefix) {
			hasExpectedOfficeDir = true
		}
		if hasContentTypes && hasExpectedOfficeDir {
			return true
		}
	}

	return false
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// Database Operations

func (s *FileService) List(countryID database.CountryID, fileType string, articleID string, limit, offset int) ([]models.File, int64, error) {
	return s.repo.ListPaginated(countryID, fileType, articleID, limit, offset)
}

func (s *FileService) FindByID(countryID database.CountryID, id uint64) (*models.File, error) {
	return s.repo.FindByID(countryID, id)
}

// FileInfoResponse is the structured payload returned by GetFileWithParent.
type FileInfoResponse struct {
	File *models.File `json:"file"`
	Item interface{}  `json:"item"`
	Type string       `json:"type"`
}

// GetFileWithParent fetches the file and its associated article or post.
// This powers the public /download/:id page.
func (s *FileService) GetFileWithParent(countryID database.CountryID, id uint64) (*FileInfoResponse, error) {
	file, item, itemType, err := s.repo.GetFileWithParent(countryID, id)
	if err != nil {
		return nil, MapError(err)
	}

	return &FileInfoResponse{
		File: file,
		Item: item,
		Type: itemType,
	}, nil
}

func (s *FileService) IncrementViewCount(countryID database.CountryID, id uint64) error {
	return ViewCounter.IncrementFileView(countryID, id)
}

func (s *FileService) CreateRecord(countryID database.CountryID, uploaded *UploadedFile, articleID *uint, postID *uint, fileName *string, fileCategory *string) (*models.File, error) {
	file := &models.File{
		FilePath:  uploaded.Path,
		FileType:  strings.TrimPrefix(uploaded.Ext, "."),
		FileName:  uploaded.Name,
		FileSize:  uploaded.Size,
		MimeType:  uploaded.MimeType,
		ArticleID: articleID,
		PostID:    postID,
	}

	if fileName != nil && *fileName != "" {
		file.FileName = *fileName
	}
	if fileCategory != nil && *fileCategory != "" {
		file.FileCategory = fileCategory
	}

	if err := s.repo.Create(countryID, file); err != nil {
		return nil, MapError(err)
	}

	return file, nil
}

// UpdateFileInput represents allowed file updates
type UpdateFileInput struct {
	FileName     string  `json:"file_name"`
	FileCategory *string `json:"file_category"`
	ArticleID    *uint   `json:"article_id"`
	PostID       *uint   `json:"post_id"`
}

func (s *FileService) UpdateRecord(countryID database.CountryID, id uint64, req *UpdateFileInput) (*models.File, error) {
	file, err := s.repo.FindByID(countryID, id)
	if err != nil {
		return nil, MapError(err)
	}

	if req.FileName != "" {
		file.FileName = req.FileName
	}
	if req.FileCategory != nil {
		file.FileCategory = req.FileCategory
	}
	if req.ArticleID != nil {
		file.ArticleID = req.ArticleID
	}
	if req.PostID != nil {
		file.PostID = req.PostID
	}

	if err := s.repo.Update(countryID, file); err != nil {
		return nil, MapError(err)
	}

	return file, nil
}

func (s *FileService) DeleteRecord(countryID database.CountryID, id uint64) error {
	file, err := s.repo.FindByID(countryID, id)
	if err != nil {
		return MapError(err)
	}

	// Delete physical file
	s.Delete(file.FilePath)

	return s.repo.Delete(countryID, file)
}
