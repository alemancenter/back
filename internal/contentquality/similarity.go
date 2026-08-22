package contentquality

import (
	"crypto/sha256"
	"encoding/hex"
	"hash/fnv"
	"html"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const (
	SimilarityKindExact    = "exact"
	SimilarityKindNear     = "near"
	SimilarityKindTemplate = "template"
)

type SimilarityDocument struct {
	Key     string
	Title   string
	Content string
}

type SimilarityOptions struct {
	MinWords                 int
	ShingleSize              int
	NearThreshold            float64
	TemplateThreshold        float64
	TemplateContainment      float64
	MinRareJaccard           float64
	MinSharedRareShingles    int
	MaxCommonShingleFraction float64
}

type SimilarityPair struct {
	LeftKey        string  `json:"left_key"`
	RightKey       string  `json:"right_key"`
	Kind           string  `json:"kind"`
	Similarity     float64 `json:"similarity"`
	Containment    float64 `json:"containment"`
	RareSimilarity float64 `json:"rare_similarity"`
	SharedShingles int     `json:"shared_shingles"`
	Fingerprint    string  `json:"fingerprint,omitempty"`
}

type SimilarityCluster struct {
	ID            string           `json:"id"`
	Kind          string           `json:"kind"`
	Members       []string         `json:"members"`
	Pairs         []SimilarityPair `json:"pairs"`
	MaxSimilarity float64          `json:"max_similarity"`
	MinSimilarity float64          `json:"min_similarity"`
}

type SimilarityReport struct {
	ScannedDocuments int                 `json:"scanned_documents"`
	IgnoredDocuments int                 `json:"ignored_documents"`
	Pairs            []SimilarityPair    `json:"pairs"`
	Clusters         []SimilarityCluster `json:"clusters"`
}

var similarityScriptStyleRe = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>`)
var similarityTagRe = regexp.MustCompile(`<[^>]+>`)

func DefaultSimilarityOptions() SimilarityOptions {
	return SimilarityOptions{
		MinWords:                 30,
		ShingleSize:              5,
		NearThreshold:            0.78,
		TemplateThreshold:        0.50,
		TemplateContainment:      0.72,
		MinRareJaccard:           0.30,
		MinSharedRareShingles:    6,
		MaxCommonShingleFraction: 0.08,
	}
}

// NormalizeForSimilarity creates a stable comparison form for Arabic educational
// content. It removes HTML, diacritics/tatweel and punctuation, normalizes common
// Alef/Ya variants, and collapses whitespace. It intentionally keeps lexical
// content and numbers so different lessons do not collapse into one fingerprint.
func NormalizeForSimilarity(value string) string {
	value = similarityScriptStyleRe.ReplaceAllString(value, " ")
	value = similarityTagRe.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	value = strings.ToLower(value)

	var b strings.Builder
	b.Grow(len(value))
	lastSpace := true
	for _, r := range value {
		switch r {
		case 'أ', 'إ', 'آ', 'ٱ':
			r = 'ا'
		case 'ى':
			r = 'ي'
		case 'ؤ':
			r = 'و'
		case 'ئ':
			r = 'ي'
		case 'ـ':
			continue
		}
		if isArabicMark(r) {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func SimilarityWordCount(value string) int {
	normalized := NormalizeForSimilarity(value)
	if normalized == "" {
		return 0
	}
	return len(strings.Fields(normalized))
}

func DetectSimilarity(documents []SimilarityDocument, options SimilarityOptions) SimilarityReport {
	opts := normalizeSimilarityOptions(options)
	prepared := make([]preparedSimilarityDocument, 0, len(documents))
	ignored := 0
	seenKeys := make(map[string]struct{}, len(documents))
	for _, document := range documents {
		key := strings.TrimSpace(document.Key)
		if key == "" {
			ignored++
			continue
		}
		if _, exists := seenKeys[key]; exists {
			ignored++
			continue
		}
		seenKeys[key] = struct{}{}
		normalized := NormalizeForSimilarity(document.Content)
		words := strings.Fields(normalized)
		if len(words) < opts.MinWords {
			ignored++
			continue
		}
		allShingles := makeShingleSet(words, opts.ShingleSize)
		if len(allShingles) == 0 {
			ignored++
			continue
		}
		hash := sha256.Sum256([]byte(normalized))
		prepared = append(prepared, preparedSimilarityDocument{
			Key:         key,
			Title:       NormalizeForSimilarity(document.Title),
			Fingerprint: hex.EncodeToString(hash[:]),
			Shingles:    allShingles,
		})
	}

	report := SimilarityReport{ScannedDocuments: len(prepared), IgnoredDocuments: ignored}
	if len(prepared) < 2 {
		return report
	}

	docFrequency := make(map[uint64]int)
	for _, document := range prepared {
		for shingle := range document.Shingles {
			docFrequency[shingle]++
		}
	}
	commonCutoff := int(float64(len(prepared))*opts.MaxCommonShingleFraction + 0.999999)
	if commonCutoff < 3 {
		commonCutoff = 3
	}
	for i := range prepared {
		prepared[i].RareShingles = make(map[uint64]struct{})
		for shingle := range prepared[i].Shingles {
			if docFrequency[shingle] <= commonCutoff {
				prepared[i].RareShingles[shingle] = struct{}{}
			}
		}
	}

	pairs := make([]SimilarityPair, 0)
	exactPairs := make(map[indexPair]struct{})
	fingerprints := make(map[string][]int)
	for i, document := range prepared {
		fingerprints[document.Fingerprint] = append(fingerprints[document.Fingerprint], i)
	}
	for fingerprint, indexes := range fingerprints {
		if len(indexes) < 2 {
			continue
		}
		for x := 0; x < len(indexes); x++ {
			for y := x + 1; y < len(indexes); y++ {
				pairIndex := orderedIndexPair(indexes[x], indexes[y])
				exactPairs[pairIndex] = struct{}{}
				pairs = append(pairs, SimilarityPair{
					LeftKey:        prepared[pairIndex.A].Key,
					RightKey:       prepared[pairIndex.B].Key,
					Kind:           SimilarityKindExact,
					Similarity:     1,
					Containment:    1,
					RareSimilarity: 1,
					SharedShingles: len(prepared[pairIndex.A].Shingles),
					Fingerprint:    fingerprint,
				})
			}
		}
	}

	postings := make(map[uint64][]int)
	for i, document := range prepared {
		for shingle := range document.RareShingles {
			postings[shingle] = append(postings[shingle], i)
		}
	}
	sharedRare := make(map[indexPair]int)
	for _, indexes := range postings {
		if len(indexes) < 2 {
			continue
		}
		for x := 0; x < len(indexes); x++ {
			for y := x + 1; y < len(indexes); y++ {
				sharedRare[orderedIndexPair(indexes[x], indexes[y])]++
			}
		}
	}

	for pairIndex, rareShared := range sharedRare {
		if rareShared < opts.MinSharedRareShingles {
			continue
		}
		if _, exact := exactPairs[pairIndex]; exact {
			continue
		}
		left := prepared[pairIndex.A]
		right := prepared[pairIndex.B]
		intersection := setIntersectionSize(left.Shingles, right.Shingles)
		if intersection == 0 {
			continue
		}
		union := len(left.Shingles) + len(right.Shingles) - intersection
		similarity := safeRatio(intersection, union)
		minSize := len(left.Shingles)
		if len(right.Shingles) < minSize {
			minSize = len(right.Shingles)
		}
		containment := safeRatio(intersection, minSize)
		rareIntersection := setIntersectionSize(left.RareShingles, right.RareShingles)
		rareUnion := len(left.RareShingles) + len(right.RareShingles) - rareIntersection
		rareSimilarity := safeRatio(rareIntersection, rareUnion)

		kind := ""
		if similarity >= opts.NearThreshold && containment >= 0.78 && rareSimilarity >= opts.MinRareJaccard {
			kind = SimilarityKindNear
		} else if similarity >= opts.TemplateThreshold && containment >= opts.TemplateContainment && rareSimilarity >= opts.MinRareJaccard && left.Title != right.Title {
			kind = SimilarityKindTemplate
		}
		if kind == "" {
			continue
		}
		pairs = append(pairs, SimilarityPair{
			LeftKey:        left.Key,
			RightKey:       right.Key,
			Kind:           kind,
			Similarity:     similarity,
			Containment:    containment,
			RareSimilarity: rareSimilarity,
			SharedShingles: intersection,
		})
	}

	sort.Slice(pairs, func(i, j int) bool {
		if similarityKindPriority(pairs[i].Kind) != similarityKindPriority(pairs[j].Kind) {
			return similarityKindPriority(pairs[i].Kind) > similarityKindPriority(pairs[j].Kind)
		}
		if pairs[i].Similarity != pairs[j].Similarity {
			return pairs[i].Similarity > pairs[j].Similarity
		}
		if pairs[i].LeftKey != pairs[j].LeftKey {
			return pairs[i].LeftKey < pairs[j].LeftKey
		}
		return pairs[i].RightKey < pairs[j].RightKey
	})
	report.Pairs = pairs
	report.Clusters = clusterSimilarityPairs(pairs)
	return report
}

type preparedSimilarityDocument struct {
	Key          string
	Title        string
	Fingerprint  string
	Shingles     map[uint64]struct{}
	RareShingles map[uint64]struct{}
}

type indexPair struct{ A, B int }

func orderedIndexPair(a, b int) indexPair {
	if a > b {
		a, b = b, a
	}
	return indexPair{A: a, B: b}
}

func normalizeSimilarityOptions(options SimilarityOptions) SimilarityOptions {
	defaults := DefaultSimilarityOptions()
	if options.MinWords <= 0 {
		options.MinWords = defaults.MinWords
	}
	if options.ShingleSize <= 1 {
		options.ShingleSize = defaults.ShingleSize
	}
	if options.NearThreshold <= 0 || options.NearThreshold > 1 {
		options.NearThreshold = defaults.NearThreshold
	}
	if options.TemplateThreshold <= 0 || options.TemplateThreshold > 1 {
		options.TemplateThreshold = defaults.TemplateThreshold
	}
	if options.TemplateContainment <= 0 || options.TemplateContainment > 1 {
		options.TemplateContainment = defaults.TemplateContainment
	}
	if options.MinRareJaccard <= 0 || options.MinRareJaccard > 1 {
		options.MinRareJaccard = defaults.MinRareJaccard
	}
	if options.MinSharedRareShingles <= 0 {
		options.MinSharedRareShingles = defaults.MinSharedRareShingles
	}
	if options.MaxCommonShingleFraction <= 0 || options.MaxCommonShingleFraction >= 1 {
		options.MaxCommonShingleFraction = defaults.MaxCommonShingleFraction
	}
	return options
}

func makeShingleSet(words []string, size int) map[uint64]struct{} {
	set := make(map[uint64]struct{})
	if len(words) < size {
		return set
	}
	for i := 0; i <= len(words)-size; i++ {
		h := fnv.New64a()
		for j := 0; j < size; j++ {
			if j > 0 {
				_, _ = h.Write([]byte{0})
			}
			_, _ = h.Write([]byte(words[i+j]))
		}
		set[h.Sum64()] = struct{}{}
	}
	return set
}

func setIntersectionSize(left, right map[uint64]struct{}) int {
	if len(left) > len(right) {
		left, right = right, left
	}
	count := 0
	for value := range left {
		if _, ok := right[value]; ok {
			count++
		}
	}
	return count
}

func safeRatio(numerator, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func isArabicMark(r rune) bool {
	return (r >= '\u0610' && r <= '\u061a') || (r >= '\u064b' && r <= '\u065f') || r == '\u0670' || (r >= '\u06d6' && r <= '\u06ed')
}

func similarityKindPriority(kind string) int {
	switch kind {
	case SimilarityKindExact:
		return 3
	case SimilarityKindNear:
		return 2
	case SimilarityKindTemplate:
		return 1
	default:
		return 0
	}
}

func clusterSimilarityPairs(pairs []SimilarityPair) []SimilarityCluster {
	if len(pairs) == 0 {
		return nil
	}
	parent := make(map[string]string)
	var find func(string) string
	find = func(key string) string {
		p, ok := parent[key]
		if !ok {
			parent[key] = key
			return key
		}
		if p != key {
			parent[key] = find(p)
		}
		return parent[key]
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		if ra < rb {
			parent[rb] = ra
		} else {
			parent[ra] = rb
		}
	}
	for _, pair := range pairs {
		union(pair.LeftKey, pair.RightKey)
	}

	membersByRoot := make(map[string]map[string]struct{})
	pairsByRoot := make(map[string][]SimilarityPair)
	for _, pair := range pairs {
		root := find(pair.LeftKey)
		if membersByRoot[root] == nil {
			membersByRoot[root] = make(map[string]struct{})
		}
		membersByRoot[root][pair.LeftKey] = struct{}{}
		membersByRoot[root][pair.RightKey] = struct{}{}
		pairsByRoot[root] = append(pairsByRoot[root], pair)
	}

	clusters := make([]SimilarityCluster, 0, len(membersByRoot))
	for root, memberSet := range membersByRoot {
		members := make([]string, 0, len(memberSet))
		for member := range memberSet {
			members = append(members, member)
		}
		sort.Strings(members)
		clusterPairs := pairsByRoot[root]
		kind := SimilarityKindTemplate
		maxSimilarity := 0.0
		minSimilarity := 1.0
		for _, pair := range clusterPairs {
			if similarityKindPriority(pair.Kind) > similarityKindPriority(kind) {
				kind = pair.Kind
			}
			if pair.Similarity > maxSimilarity {
				maxSimilarity = pair.Similarity
			}
			if pair.Similarity < minSimilarity {
				minSimilarity = pair.Similarity
			}
		}
		idHash := sha256.Sum256([]byte(strings.Join(members, "|")))
		clusters = append(clusters, SimilarityCluster{
			ID:            hex.EncodeToString(idHash[:])[:16],
			Kind:          kind,
			Members:       members,
			Pairs:         clusterPairs,
			MaxSimilarity: maxSimilarity,
			MinSimilarity: minSimilarity,
		})
	}
	sort.Slice(clusters, func(i, j int) bool {
		if similarityKindPriority(clusters[i].Kind) != similarityKindPriority(clusters[j].Kind) {
			return similarityKindPriority(clusters[i].Kind) > similarityKindPriority(clusters[j].Kind)
		}
		if clusters[i].MaxSimilarity != clusters[j].MaxSimilarity {
			return clusters[i].MaxSimilarity > clusters[j].MaxSimilarity
		}
		return clusters[i].ID < clusters[j].ID
	})
	return clusters
}
