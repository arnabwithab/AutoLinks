// ----- equity distribution evaluation @ backend/eval/equity/main.go -----
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sort"

	"github.com/arnabwithab/AutoLinks/backend/internal/config"
)

var apiBase string

func init() {
	apiBase = os.Getenv("EVAL_API_URL")
	if apiBase == "" {
		apiBase = "http://localhost:8000/api/v1"
	}
	config.Get("", "")
}

var testDrafts = []string{
	"When we finally achieve Artificial General Intelligence, the timeline might accelerate much faster than we anticipate.",
	"The jump from human-level AI to Artificial Superintelligence could happen in a matter of hours, catching humanity off guard.",
	"Narrow AI is already everywhere, but general intelligence requires a fundamental breakthrough in reasoning.",
	"The concept of the Turing Test is becoming less relevant as large language models demonstrate emergent behaviors.",
	"If biological intelligence is just a computational process, there is no physical law preventing machines from replicating it.",
	"Given the vastness of the observable universe, the Fermi Paradox asks the obvious question: where is everybody?",
	"The Great Filter theory suggests there is an evolutionary step so improbable that almost no civilization survives it.",
	"If we are the first civilization to pass the Great Filter, humanity has a massive responsibility to colonize the galaxy.",
	"Building a Dyson Sphere would require dismantling entire planets to capture the energy output of a star.",
	"The Kardashev Scale categorizes advanced civilizations based on their ability to harness energy from their planet, star, or galaxy.",
	"Making humanity a multi-planetary species acts as a biological hard drive backup in case Earth experiences an extinction event.",
	"SpaceX's ultimate goal is driving down the cost of payload to orbit to enable Mars colonization.",
	"The Drake Equation gives us a mathematical framework to estimate the number of active, communicative extraterrestrial civilizations.",
	"If faster-than-light travel is impossible, galactic colonization will rely on generational ships or advanced cryonics.",
	"The Instant Gratification Monkey only cares about maximizing the ease and pleasure of the current moment.",
	"When a deadline approaches, the Panic Monster wakes up, forcing the procrastinator into a state of hyper-focus.",
	"The Rational Decision Maker in our brain often loses control of the steering wheel when a difficult task presents itself.",
	"Human brains evolved for a tribal environment, making modern social media a toxic hyper-stimulus for our ancient hardware.",
	"We all have a finite number of weeks in our life calendar, yet we spend so many of them entirely on autopilot.",
	"High-bandwidth brain-machine interfaces like Neuralink could eventually allow for non-verbal conceptual telepathy.",
	"The human neocortex is responsible for our highest-level reasoning, separating us from the purely reactive limbic system.",
	"If you compress the entire history of Earth into a single calendar year, modern humans only appear in the final seconds of December 31st.",
	"The exponential growth of technological progress means the 21st century will experience far more change than the previous millennium.",
	"Understanding deep time requires breaking past our cognitive biases, which are tuned to understand days and years, not eons.",
	"The agricultural revolution fundamentally changed human social structures, moving us from egalitarian tribes to hierarchical societies.",
	"We are currently living in the Anthropocene, the first geological epoch defined entirely by the impact of a single species.",
	"The story of human progress is largely the story of our increasing ability to capture, store, and transmit information.",
	"Writing was the first great technological leap that allowed human knowledge to compound across generations.",
	"The industrial revolution replaced biological muscle power with the immense stored energy of fossil fuels.",
	"When you look at a family tree going back hundreds of generations, you realize how genetically interconnected the entire human race is.",
	"The concept of emergence explains how simple rules at a micro level can create incredibly complex behaviors at a macro level.",
	"Picking a career path is often paralyzed by the fear of closing doors, but staying in the hallway indefinitely is the worst option.",
	"First-principles thinking requires stripping a problem down to its fundamental physical truths and building up from there.",
	"The sunk cost fallacy keeps people trapped in unfulfilling jobs simply because they have already invested years into the path.",
	"True deep work requires disconnecting completely from the constant dopamine drip of the modern internet.",
	"The difference between a growth mindset and a fixed mindset determines how you handle inevitable failures in a new venture.",
	"Imposter syndrome is incredibly common among high achievers because they are hyper-aware of the gap between their taste and their current output.",
	"Choosing a life partner is essentially picking your permanent roommate, financial partner, and co-parent for the next fifty years.",
	"The pursuit of happiness often backfires; meaning and fulfillment are usually byproducts of solving difficult, worthwhile problems.",
}

func computeGini(linkCounts []int) float64 {
	if len(linkCounts) == 0 {
		return 0.0
	}

	sorted := make([]int, len(linkCounts))
	copy(sorted, linkCounts)
	sort.Ints(sorted)

	n := len(sorted)
	var cumsum float64
	for i, count := range sorted {
		cumsum += float64((i + 1) * count)
	}

	total := 0
	for _, c := range sorted {
		total += c
	}
	if total == 0 {
		return 0.0
	}

	gini := (2*cumsum)/(float64(n)*float64(total)) - float64(n+1)/float64(n)
	return math.Max(0.0, math.Min(1.0, gini))
}

func applyRecommendations(linkGraph map[string]int, recommendedURLs []string) map[string]int {
	projected := make(map[string]int)
	for k, v := range linkGraph {
		projected[k] = v
	}

	for _, url := range recommendedURLs {
		if _, ok := projected[url]; ok {
			projected[url]++
		}
	}

	return projected
}

func computeOrphanReduction(recommendedURLs []string, linkGraph map[string]int) float64 {
	orphanURLs := make(map[string]bool)
	for url, count := range linkGraph {
		if count == 0 {
			orphanURLs[url] = true
		}
	}

	if len(orphanURLs) == 0 {
		return 0.0
	}

	recSet := make(map[string]bool)
	for _, url := range recommendedURLs {
		recSet[url] = true
	}

	rescued := 0
	for url := range orphanURLs {
		if recSet[url] {
			rescued++
		}
	}

	return float64(rescued) / float64(len(orphanURLs))
}

func buildSyntheticLinkGraph(totalURLs, seed int) map[string]int {
	rng := rand.New(rand.NewSource(int64(seed)))
	orphanCount := max(1, int(float64(totalURLs)*0.15))
	lowCount := max(1, int(float64(totalURLs)*0.35))
	midCount := max(1, int(float64(totalURLs)*0.30))
	highCount := max(1, totalURLs-orphanCount-lowCount-midCount)

	graph := make(map[string]int)
	idx := 1

	for range orphanCount {
		graph[fmt.Sprintf("https://synthetic.test/article-%03d", idx)] = 0
		idx++
	}
	for range lowCount {
		graph[fmt.Sprintf("https://synthetic.test/article-%03d", idx)] = rng.Intn(3) + 1
		idx++
	}
	for range midCount {
		graph[fmt.Sprintf("https://synthetic.test/article-%03d", idx)] = rng.Intn(9) + 4
		idx++
	}
	for range highCount {
		graph[fmt.Sprintf("https://synthetic.test/article-%03d", idx)] = rng.Intn(56) + 25
		idx++
	}

	return graph
}

func fetchLinkGraph() (map[string]int, error) {
	resp, err := http.Get(apiBase + "/link-graph")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data struct {
		LinkGraph map[string]int `json:"link_graph"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data.LinkGraph, nil
}

func fetchRecommendations(draftText string, alpha float64) []string {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"text":           draftText,
		"alpha":          alpha,
		"min_similarity": 0.0,
	})

	resp, err := http.Post(apiBase+"/recommend", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		fmt.Printf("Draft error: %v\n", err)
		return nil
	}
	defer resp.Body.Close()

	var data struct {
		Recommendations []struct {
			SuggestedURL string `json:"suggested_url"`
		} `json:"recommendations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil
	}

	var urls []string
	for _, r := range data.Recommendations {
		urls = append(urls, r.SuggestedURL)
	}
	return urls
}

func runLiveEquityEvaluation() {
	linkGraph, err := fetchLinkGraph()
	if err != nil || len(linkGraph) == 0 {
		fmt.Println("Link graph is empty. Ingest a sitemap before running the eval.")
		return
	}

	numDrafts := 50

	var baselineURLs []string
	var equityURLs []string
	baselineDrafts := 0
	equityDrafts := 0

	fmt.Println("Running recommendations with alpha=1.0 (baseline)")
	for i := 0; i < numDrafts; i++ {
		draft := testDrafts[i%len(testDrafts)]
		urls := fetchRecommendations(draft, 1.0)
		baselineURLs = append(baselineURLs, urls...)
		if len(urls) > 0 {
			baselineDrafts++
		}
		fmt.Printf("Baseline draft %d/%d: %d recommendations\n", i+1, numDrafts, len(urls))
	}

	fmt.Println("Running recommendations with alpha=0.7 (equity-aware)")
	for i := 0; i < numDrafts; i++ {
		draft := testDrafts[i%len(testDrafts)]
		urls := fetchRecommendations(draft, 0.7)
		equityURLs = append(equityURLs, urls...)
		if len(urls) > 0 {
			equityDrafts++
		}
		fmt.Printf("Equity-aware draft %d/%d: %d recommendations\n", i+1, numDrafts, len(urls))
	}

	emitReport(linkGraph, baselineURLs, equityURLs, baselineDrafts, equityDrafts, "Live")
}

func runSyntheticEquityEvaluation(totalURLs, seed int) {
	linkGraph := buildSyntheticLinkGraph(totalURLs, seed)
	numDrafts := 50

	var baselineURLs []string
	var equityURLs []string

	rng := rand.New(rand.NewSource(int64(seed)))

	orphanURLs := []string{}
	lowURLs := []string{}
	midURLs := []string{}
	highURLs := []string{}

	for url, count := range linkGraph {
		switch {
		case count == 0:
			orphanURLs = append(orphanURLs, url)
		case count <= 3:
			lowURLs = append(lowURLs, url)
		case count <= 12:
			midURLs = append(midURLs, url)
		default:
			highURLs = append(highURLs, url)
		}
	}

	scoreForAlpha := func(similarity float64, inboundLinks int, alpha float64) float64 {
		equityNeed := 1.0 / (1.0 + float64(inboundLinks))
		return alpha*similarity + (1-alpha)*equityNeed
	}

	for range numDrafts {
		var candidates []struct {
			url   string
			score float64
		}

		addCandidates := func(pool []string, scoreMin, scoreMax float64, count int) {
			rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
			for i := 0; i < count && i < len(pool); i++ {
				score := scoreMin + rng.Float64()*(scoreMax-scoreMin)
				candidates = append(candidates, struct {
					url   string
					score float64
				}{pool[i], score})
			}
		}

		addCandidates(highURLs, 0.93, 0.99, 3)
		addCandidates(midURLs, 0.84, 0.92, 3)
		addCandidates(lowURLs, 0.76, 0.88, 3)
		addCandidates(orphanURLs, 0.68, 0.82, 3)

		for _, alpha := range []float64{1.0, 0.7} {
			sort.Slice(candidates, func(i, j int) bool {
				return scoreForAlpha(candidates[i].score, linkGraph[candidates[i].url], alpha) >
					scoreForAlpha(candidates[j].score, linkGraph[candidates[j].url], alpha)
			})

			seen := make(map[string]bool)
			var selected []string
			for _, c := range candidates {
				if seen[c.url] {
					continue
				}
				seen[c.url] = true
				selected = append(selected, c.url)
				if len(selected) >= 5 {
					break
				}
			}

			if alpha == 1.0 {
				baselineURLs = append(baselineURLs, selected...)
			} else {
				equityURLs = append(equityURLs, selected...)
			}
		}
	}

	emitReport(linkGraph, baselineURLs, equityURLs, numDrafts, numDrafts, "Synthetic")
}

func emitReport(linkGraph map[string]int, baselineRecs, equityRecs []string, baselineDrafts, equityDrafts int, mode string) {
	baselineGraph := applyRecommendations(linkGraph, baselineRecs)
	equityGraph := applyRecommendations(linkGraph, equityRecs)

	baselineCounts := values(baselineGraph)
	equityCounts := values(equityGraph)

	baselineGini := computeGini(baselineCounts)
	equityGini := computeGini(equityCounts)

	baselineOrphan := computeOrphanReduction(baselineRecs, linkGraph)
	equityOrphan := computeOrphanReduction(equityRecs, linkGraph)

	baselineUnique := uniqueCount(baselineRecs)
	equityUnique := uniqueCount(equityRecs)

	fmt.Println()
	fmt.Println("============================================================")
	fmt.Printf("EQUITY DISTRIBUTION EVALUATION RESULTS (%s)\n", mode)
	fmt.Println("============================================================")
	fmt.Printf("Total Drafts:        50\n")
	fmt.Println("------------------------------------------------------------")
	fmt.Println("BASELINE (alpha = 1.0, pure similarity)")
	fmt.Printf("  Draft Coverage:        %d/50\n", baselineDrafts)
	fmt.Printf("  Total Recommendations: %d\n", len(baselineRecs))
	fmt.Printf("  Unique URLs:           %d\n", baselineUnique)
	fmt.Printf("  Gini Coefficient:      %.4f\n", baselineGini)
	fmt.Printf("  Orphan Reduction:      %.2f%%\n", baselineOrphan*100)
	fmt.Println("------------------------------------------------------------")
	fmt.Println("EQUITY-AWARE (alpha = 0.7)")
	fmt.Printf("  Draft Coverage:        %d/50\n", equityDrafts)
	fmt.Printf("  Total Recommendations: %d\n", len(equityRecs))
	fmt.Printf("  Unique URLs:           %d\n", equityUnique)
	fmt.Printf("  Gini Coefficient:      %.4f\n", equityGini)
	fmt.Printf("  Orphan Reduction:      %.2f%%\n", equityOrphan*100)
	fmt.Println("------------------------------------------------------------")
	fmt.Println("COMPARISON")
	fmt.Printf("  Gini Improvement:     %.4f (%s)\n",
		baselineGini-equityGini,
		map[bool]string{true: "better", false: "worse"}[equityGini < baselineGini])
	fmt.Printf("  Orphan Lift:          %.2f%%\n", (equityOrphan-baselineOrphan)*100)
	fmt.Printf("  URL Distribution:     %+d unique URLs\n", equityUnique-baselineUnique)
	fmt.Println("============================================================")

	if equityGini < baselineGini && equityOrphan > baselineOrphan {
		fmt.Println("RESULT: Equity-aware re-ranking SUCCESSFUL")
	} else {
		fmt.Println("RESULT: Equity-aware re-ranking needs tuning")
	}
}

func values(m map[string]int) []int {
	var v []int
	for _, val := range m {
		v = append(v, val)
	}
	return v
}

func uniqueCount(urls []string) int {
	m := make(map[string]bool)
	for _, u := range urls {
		m[u] = true
	}
	return len(m)
}

func main() {
	mode := flag.String("mode", "live", "Evaluation mode: live or synthetic")
	syntheticURLs := flag.Int("synthetic-urls", 200, "Number of synthetic URLs")
	seed := flag.Int("seed", 42, "Random seed")
	flag.Parse()

	fmt.Printf("Starting equity distribution evaluation with %d drafts\n", 50)
	fmt.Printf("API Base URL: %s\n", apiBase)

	if *mode == "synthetic" {
		runSyntheticEquityEvaluation(*syntheticURLs, *seed)
		return
	}

	resp, err := http.Get(apiBase + "/health")
	if err != nil || resp.StatusCode != http.StatusOK {
		fmt.Println("API not available. Start the server first.")
		if resp != nil {
			resp.Body.Close()
		}
		return
	}
	resp.Body.Close()

	runLiveEquityEvaluation()
}
