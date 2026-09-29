// ----- precision evaluation with Groq LLM judge @ backend/eval/precision/main.go -----
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/config"
)

var apiBase string

func init() {
	apiBase = os.Getenv("EVAL_API_URL")
	if apiBase == "" {
		apiBase = "http://localhost:8000/api/v1"
	}
}

type evalCase struct {
	Text     string `json:"text"`
	Tripwire bool   `json:"tripwire"`
}

var evaluationCases = []evalCase{
	{Text: "When we finally achieve Artificial General Intelligence, the timeline might accelerate much faster than we anticipate.", Tripwire: false},
	{Text: "The jump from human-level AI to Artificial Superintelligence could happen in a matter of hours, catching humanity off guard.", Tripwire: false},
	{Text: "Bostrom's concept of an intelligence explosion is critical when calculating our survival odds alongside advanced AI.", Tripwire: false},
	{Text: "If an AI is programmed to optimize paperclips, a superintelligent system might consume the entire galaxy's resources to do it.", Tripwire: false},
	{Text: "The timeline to AGI is heavily debated, but the median estimate among ML researchers is shrinking rapidly.", Tripwire: false},
	{Text: "Aligning an artificial superintelligence with human values is arguably the most important technical problem in history.", Tripwire: false},
	{Text: "We are currently standing on the tripwire of the AI revolution, right before the exponential curve goes vertical.", Tripwire: false},
	{Text: "Narrow AI is already everywhere, but general intelligence requires a fundamental breakthrough in reasoning.", Tripwire: false},
	{Text: "The concept of the Turing Test is becoming less relevant as large language models demonstrate emergent behaviors.", Tripwire: false},
	{Text: "If biological intelligence is just a computational process, there is no physical law preventing machines from replicating it.", Tripwire: false},
	{Text: "Given the vastness of the observable universe, the Fermi Paradox asks the obvious question: where is everybody?", Tripwire: false},
	{Text: "The Great Filter theory suggests there is an evolutionary step so improbable that almost no civilization survives it.", Tripwire: false},
	{Text: "If we are the first civilization to pass the Great Filter, humanity has a massive responsibility to colonize the galaxy.", Tripwire: false},
	{Text: "Building a Dyson Sphere would require dismantling entire planets to capture the energy output of a star.", Tripwire: false},
	{Text: "The Kardashev Scale categorizes advanced civilizations based on their ability to harness energy from their planet, star, or galaxy.", Tripwire: false},
	{Text: "Making humanity a multi-planetary species acts as a biological hard drive backup in case Earth experiences an extinction event.", Tripwire: false},
	{Text: "SpaceX's ultimate goal is driving down the cost of payload to orbit to enable Mars colonization.", Tripwire: false},
	{Text: "The Drake Equation gives us a mathematical framework to estimate the number of active, communicative extraterrestrial civilizations.", Tripwire: false},
	{Text: "If faster-than-light travel is impossible, galactic colonization will rely on generational ships or advanced cryonics.", Tripwire: false},
	{Text: "The concept of the Dark Forest suggests that advanced civilizations stay silent to avoid being destroyed by apex predators.", Tripwire: false},
	{Text: "The Instant Gratification Monkey only cares about maximizing the ease and pleasure of the current moment.", Tripwire: false},
	{Text: "When a deadline approaches, the Panic Monster wakes up, forcing the procrastinator into a state of hyper-focus.", Tripwire: false},
	{Text: "The Rational Decision Maker in our brain often loses control of the steering wheel when a difficult task presents itself.", Tripwire: false},
	{Text: "Overcoming the Social Survival Mammoth means realizing that most people aren't actually paying attention to your mistakes.", Tripwire: false},
	{Text: "Human brains evolved for a tribal environment, making modern social media a toxic hyper-stimulus for our ancient hardware.", Tripwire: false},
	{Text: "The concept of the 'Dark Playground' describes the guilt-ridden leisure time you experience when you should be working.", Tripwire: false},
	{Text: "We all have a finite number of weeks in our life calendar, yet we spend so many of them entirely on autopilot.", Tripwire: false},
	{Text: "The 'Cook vs. Chef' analogy perfectly illustrates the difference between blindly following the crowd and reasoning from first principles.", Tripwire: false},
	{Text: "High-bandwidth brain-machine interfaces like Neuralink could eventually allow for non-verbal conceptual telepathy.", Tripwire: false},
	{Text: "The human neocortex is responsible for our highest-level reasoning, separating us from the purely reactive limbic system.", Tripwire: false},
	{Text: "If you compress the entire history of Earth into a single calendar year, modern humans only appear in the final seconds of December 31st.", Tripwire: false},
	{Text: "The exponential growth of technological progress means the 21st century will experience far more change than the previous millennium.", Tripwire: false},
	{Text: "Understanding deep time requires breaking past our cognitive biases, which are tuned to understand days and years, not eons.", Tripwire: false},
	{Text: "The agricultural revolution fundamentally changed human social structures, moving us from egalitarian tribes to hierarchical societies.", Tripwire: false},
	{Text: "We are currently living in the Anthropocene, the first geological epoch defined entirely by the impact of a single species.", Tripwire: false},
	{Text: "The story of human progress is largely the story of our increasing ability to capture, store, and transmit information.", Tripwire: false},
	{Text: "Writing was the first great technological leap that allowed human knowledge to compound across generations.", Tripwire: false},
	{Text: "The industrial revolution replaced biological muscle power with the immense stored energy of fossil fuels.", Tripwire: false},
	{Text: "When you look at a family tree going back hundreds of generations, you realize how genetically interconnected the entire human race is.", Tripwire: false},
	{Text: "The concept of emergence explains how simple rules at a micro level can create incredibly complex behaviors at a macro level.", Tripwire: false},
	{Text: "She sliced the apple into thin wedges for the tart filling and sprinkled cinnamon on top.", Tripwire: true},
	{Text: "Apple reported stronger-than-expected quarterly earnings after iPhone sales rebounded.", Tripwire: true},
	{Text: "The python curled beneath the heat lamp while the zookeeper checked its scales.", Tripwire: true},
	{Text: "Python makes it easy to prototype data pipelines before hardening them for production.", Tripwire: true},
	{Text: "The jaguar moved silently through the rainforest undergrowth before leaping onto a branch.", Tripwire: true},
	{Text: "Jaguar is repositioning some of its luxury vehicles around electrification.", Tripwire: true},
	{Text: "Mercury is the closest planet to the sun and has extreme temperature swings.", Tripwire: true},
	{Text: "Mercury emissions from coal plants can accumulate in aquatic food chains.", Tripwire: true},
	{Text: "Java growers are worried that climate volatility could reduce bean quality this season.", Tripwire: true},
	{Text: "Java remains common in large enterprise systems that value mature tooling and backward compatibility.", Tripwire: true},
}

type recommendation struct {
	ExactPhrase    string `json:"exact_phrase"`
	ContextSnippet string `json:"context_snippet"`
	SuggestedURL   string `json:"suggested_url"`
}

type judgeRequest struct {
	Model       string         `json:"model"`
	Messages    []judgeMessage `json:"messages"`
	Temperature float64        `json:"temperature"`
}

type judgeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type judgeResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type verdictPayload struct {
	Verdict   string `json:"verdict"`
	Rationale string `json:"rationale"`
}

func fetchRecommendations(draftText string) []recommendation {
	reqBody, _ := json.Marshal(map[string]string{"text": draftText})
	resp, err := http.Post(apiBase+"/recommend", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		fmt.Printf("Draft fetch error: %v\n", err)
		return nil
	}
	defer resp.Body.Close()

	var data struct {
		Recommendations []recommendation `json:"recommendations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil
	}
	return data.Recommendations
}

func judgeRecommendation(groqAPIKey, draftText string, rec recommendation) *verdictPayload {
	messages := []judgeMessage{
		{
			Role: "system",
			Content: "You are grading an internal-link recommendation for semantic precision. " +
				"Decide whether the suggested destination is semantically accurate and highly helpful " +
				"for the source context. Return JSON only with keys verdict and rationale. " +
				"verdict must be YES or NO. Be strict and prefer NO if the match is vague, off-topic, " +
				"or only loosely related.",
		},
		{
			Role: "user",
			Content: fmt.Sprintf(
				"Evaluate this internal-link recommendation.\n\n"+
					"Full source draft: %s\n"+
					"Exact phrase selected for linking: %s\n"+
					"Suggested URL: %s\n"+
					"Target chunk snippet from retrieval: %s\n\n"+
					"Respond as JSON like {\"verdict\":\"YES\",\"rationale\":\"short explanation\"}",
				draftText, rec.ExactPhrase, rec.SuggestedURL, rec.ContextSnippet,
			),
		},
	}

	reqBody, _ := json.Marshal(judgeRequest{
		Model:       config.GroqModel(),
		Messages:    messages,
		Temperature: 0,
	})

	groqURL := config.GroqURL()
	req, _ := http.NewRequest("POST", groqURL, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+groqAPIKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Judge request error: %v\n", err)
		return nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var judgeResp judgeResponse
	if err := json.Unmarshal(body, &judgeResp); err != nil {
		fmt.Printf("Judge response parse error: %v\n", err)
		return nil
	}

	if len(judgeResp.Choices) == 0 {
		return nil
	}

	content := judgeResp.Choices[0].Message.Content
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	}

	var verdict verdictPayload
	if err := json.Unmarshal([]byte(content), &verdict); err != nil {
		fmt.Printf("Verdict parse error: %v, content: %s\n", err, content)
		return nil
	}

	return &verdict
}

func main() {
	fmt.Printf("Starting precision evaluation with %d drafts\n", len(evaluationCases))
	fmt.Printf("API Base URL: %s\n", apiBase)

	groqAPIKey := config.GroqAPIKey()
	if groqAPIKey == "" {
		fmt.Println("GROQ_API_KEY is required to run Eval 2")
		os.Exit(1)
	}

	resp, err := http.Get(apiBase + "/health")
	if err != nil || resp.StatusCode != http.StatusOK {
		fmt.Println("API not available. Start the server first.")
		if resp != nil {
			resp.Body.Close()
		}
		os.Exit(1)
	}
	resp.Body.Close()

	judgedRecommendations := 0
	yesCount := 0
	noCount := 0
	draftsWithRecommendations := 0
	tripwireJudged := 0
	tripwireYes := 0
	failedDrafts := 0

	for draftIndex, c := range evaluationCases {
		recommendations := fetchRecommendations(c.Text)
		if recommendations == nil {
			failedDrafts++
			fmt.Printf("Eval 2 draft %d fetch error\n", draftIndex+1)
			continue
		}

		if len(recommendations) > 0 {
			draftsWithRecommendations++
		}

		for _, rec := range recommendations {
			verdict := judgeRecommendation(groqAPIKey, c.Text, rec)
			if verdict == nil {
				continue
			}

			judgedRecommendations++
			if verdict.Verdict == "YES" {
				yesCount++
			} else {
				noCount++
			}

			if c.Tripwire {
				tripwireJudged++
				if verdict.Verdict == "YES" {
					tripwireYes++
				}
			}

			fmt.Printf("Groq judge verdict=%s url=%s\n", verdict.Verdict, rec.SuggestedURL)
		}

		fmt.Printf("Eval 2 draft %d/%d: tripwire=%v recommendations=%d\n",
			draftIndex+1, len(evaluationCases), c.Tripwire, len(recommendations))
	}

	if judgedRecommendations == 0 {
		fmt.Println("No recommendations were judged - cannot compute Eval 2 metrics")
		return
	}

	overallYesRate := float64(yesCount) / float64(judgedRecommendations)
	tripwireYesRate := 0.0
	if tripwireJudged > 0 {
		tripwireYesRate = float64(tripwireYes) / float64(tripwireJudged)
	}
	passThreshold := 0.90
	overallPass := overallYesRate >= passThreshold
	tripwirePass := tripwireYesRate >= passThreshold

	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println("AI PRECISION EVALUATION RESULTS (Eval 2)")
	fmt.Println("============================================================")
	fmt.Printf("Total Drafts:            %d\n", len(evaluationCases))
	fmt.Printf("Drafts With Recs:        %d/%d\n", draftsWithRecommendations, len(evaluationCases))
	fmt.Printf("Failed Drafts:           %d\n", failedDrafts)
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("Recommendations Judged:  %d\n", judgedRecommendations)
	fmt.Printf("YES Verdicts:            %d\n", yesCount)
	fmt.Printf("NO Verdicts:             %d\n", noCount)
	fmt.Printf("Overall YES Rate:        %.2f%%\n", overallYesRate*100)
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("Tripwire Judged:         %d\n", tripwireJudged)
	fmt.Printf("Tripwire YES Rate:       %.2f%%\n", tripwireYesRate*100)
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("Overall Threshold:       %.0f%%\n", passThreshold*100)
	overallStatus := "PASS"
	if !overallPass {
		overallStatus = "FAIL"
	}
	fmt.Printf("Overall Status:          %s\n", overallStatus)
	tripwireStatus := "PASS"
	if !tripwirePass {
		tripwireStatus = "FAIL"
	}
	fmt.Printf("Tripwire Status:         %s\n", tripwireStatus)
	fmt.Println("============================================================")
}
