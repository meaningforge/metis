package s2sbench

import (
	"fmt"
	"sort"
	"strings"
)

// PromptVersion changes whenever any frozen benchmark prompt text or surrounding
// interaction protocol changes. v0.37 keeps the frozen 25-question sample and
// multi-model world, isolates every question in a fresh clean Agent environment,
// shares only canonical arm assets between questions, and pairs raw/Metis
// collection in the same repetition/scenario order. Exact single code fences
// are treated as presentation wrappers, and each Agent's benchmark MCP remains
// enabled inside the otherwise isolated runtime. The Metis MCP exposes concise
// server-scoped tool names without repeating the server name. Both arms use the
// Agent's same native capabilities and tells both arms the identical shared-question
// tool budget so the Agent can plan within it. Raw exposes the canonical semantic-project
// files while Metis receives a clean workspace and adds only the complete MCP
// tool surface discovered at runtime. Both arms also receive the same explicit
// runtime boundary: the harness executes and validates answers, so the Agent
// must not query benchmark data or inspect the host environment. It also exposes
// model-scoped dimension discovery for metric-free questions and distinguishes
// visible output metrics from metrics used only in filters. The shared semantic
// project also declares authored value-encoding semantics such as ISO country
// codes identically to both arms. The compile schema explicitly describes typed
// filter operators, value arity, and multi-key sort precedence. Metis discovery
// uses compact list tools plus optional selected-asset and relationship detail
// tools, all discovered dynamically through MCP. Each question has one shared
// 60-second wall-clock budget across its first attempt, execution check, and
// any repair; a timeout is terminal and scored failed. The Pi MCP bridge
// faithfully propagates the product's initialize instructions without adding
// benchmark-specific Agent guidance.
// v0.34 added the production get_dimension_values tool to server-discovered
// capabilities while preserving the same questions and neutral call policy.
// v0.35 propagates the product's general guidance to use that tool when a user
// term may differ from a canonical stored member, such as a name versus a code.
// v0.36 clarifies two requested output shapes, adds shared authored semantics
// for year-start and order-state concepts, and propagates general guidance for
// complete request construction and point-in-time relationship discovery.
// v0.37 makes malformed trailing-output repair actionable without accepting or
// silently rewriting an invalid Agent answer. v0.38 makes the Pi bridge honor
// the MCP server's complete Agent-facing sql_statement text representation;
// strict answer validation remains unchanged.
// v0.39 makes list_metrics explain the general semantic-selection rule for
// similarly named metrics with different typed constraints. v0.40 makes
// candidate ranking explicitly non-authoritative and requires exact-ref
// selection rather than inferred business-language aliases. v0.41 shortens
// MCP tool descriptions, moves parameter mechanics into InputSchema, and
// removes copy/paste wording from compile output without changing its content.
const PromptVersion = "v0.41"

// Question is the single natural-language phrasing of a scenario, asked
// identically on both paths.
//
// Each one states what a person wants, in the words a person would use, and
// never how to compute it. That rule is the experiment: the whole claim under
// test is that Metis carries semantics the model would otherwise have to
// reconstruct, so a question that explains the semantics has already done the
// work being measured. "Revenue to date, by quarter and region" is the
// question; "a running total of revenue accumulated from the earliest order"
// is the answer, and putting it in the prompt would quietly hand path A the
// thing path B is supposed to provide.
//
// The phrasings are frozen. Rewording one is a new PromptVersion, not an edit.
var Questions = map[string]string{
	// silent_semantics
	"conversion_rate_by_campaign":                        "For each campaign, what share of people who signed up went on to purchase?",
	"cumulative_metric_by_quarter_and_region":            "Show revenue to date for each quarter and region.",
	"custom_calendar_dense_missing_period":               "For every fiscal week in our calendar, what was revenue in the fiscal week before it?",
	"custom_offset_to_grain_missing_boundary_stays_zero": "For each fiscal week, what was revenue as of the start of that fiscal year?",
	"offset_to_grain_missing_boundary_stays_zero":        "For each month, what was revenue as of the start of that year?",
	"semi_additive_first_skip_null":                      "For each warehouse, what was the earliest recorded inventory level, ignoring days with no reading?",
	"semi_additive_queried_week":                         "What was the inventory balance for each week?",
	"semi_additive_window_group_sum":                     "What is the total inventory across all warehouses?",
	"time_offset_previous_quarter":                       "For each quarter, what was revenue in the quarter before it?",

	// fanout
	"derived_null_negative_inputs":        "What are our revenue, discounts, and contribution margin overall?",
	"independent_multi_source_at_grain":   "What are revenue and cost for each month?",
	"metric_filter_derived_metric":        "Revenue by order status, limited to statuses whose contribution margin is between 100 and 10000.",
	"metric_filter_with_dimension_filter": "Show order status and revenue for paid orders, retaining only statuses whose revenue is greater than 100.",
	"multi_hop_repeated_dimension_values": "Revenue by country.",
	"multiple_metrics_joined_dimension":   "For each customer region, what are total revenue and order count?",
	"relationship_unmatched_facts":        "How much revenue and how many orders are associated with each known region?",
	"temporal_join_open_ended_version":    "Return the order ID and customer tier that applied to order o_current at the time of the order.",

	// edge
	"distinct_dimension_values":  "Which regions do we operate in within Japan? List them alphabetically, at most 10.",
	"filters_order_limit":        "Revenue by order status for paid orders placed during 2026, highest revenue first, breaking ties by status alphabetically, at most 25 rows.",
	"order_by_metric_ungrouped":  "What is total revenue, sorted from highest to lowest?",
	"ordered_ties_secondary_key": "Revenue by order status, excluding pending orders, highest revenue first, breaking ties by status alphabetically, top 3.",

	// control
	"aggregation_variants":            "What are the average, smallest, and largest order amounts?",
	"multiple_filters_same_dimension": "Revenue by order status, excluding cancelled, pending, and fraud orders.",
	"multiple_metrics_with_filters":   "Revenue and the number of orders by status, for paid orders placed on or after 1 January 2026.",
	"time_filter_with_month_grain":    "Monthly revenue for the first quarter of 2026.",
}

// Path names the two arms. They differ in what the agent may read and call,
// and in nothing else: same question, same tool budget, same repair rules,
// same oracle, same external agent identity.
type Path string

const (
	// PathRawAssets: the agent receives the project's Ossie model file and the
	// physical schema in its isolated workspace and authors SQL directly.
	PathRawAssets Path = "raw_assets"

	// PathMetis: the agent receives no raw semantic model and gets Metis MCP as
	// its semantic interface. It returns the compile result's sql_statement
	// object; S2SBench passes SQL and parameters separately to the driver.
	PathMetis Path = "metis"
)

// Paths in fixed order, for reports and iteration.
var Paths = []Path{PathRawAssets, PathMetis}

// TaskBudget is identical on both paths. Making it identical is what keeps the
// comparison about semantics rather than about how much rope each arm was
// given -- an arm allowed more attempts will look better for a reason that has
// nothing to do with the thesis.
type TaskBudget struct {
	// ToolCalls caps all externally observable agent tool calls across the complete
	// question, including any repair, file/shell operations, and MCP calls.
	ToolCalls int
	// Attempts caps total attempts at a question, including the first. An
	// attempt after a failure may see the error it caused and nothing else.
	Attempts int
	// QuestionTimeoutMS caps the complete question lifecycle, including the
	// first attempt, execution validation, and any repair. Zero leaves the
	// limit unset for deterministic unit seams; live frozen runs require it.
	QuestionTimeoutMS int
	// Repetitions is how many independent runs of the whole question are
	// recorded, to separate a model's spread from a real difference between
	// paths. One run per question measures a sample of size one and cannot
	// distinguish the two.
	Repetitions int
}

// FrozenBudget is the v0 budget. Attempts is 2 rather than 1 because
// first-try success and eventual success are different findings and both are
// worth having; Repetitions is 5 because a difference smaller than the
// run-to-run spread is not a difference, and 5 is the smallest count that
// shows the spread at all.
var FrozenBudget = TaskBudget{ToolCalls: 30, Attempts: 2, QuestionTimeoutMS: 60_000, Repetitions: 5}

// SmokeToolCalls uses the same shared-question tool limit as diagnostic and
// benchmark runs while retaining smoke's single repetition.
const SmokeToolCalls = 30

// QuestionFor returns the frozen phrasing for a selected scenario.
func QuestionFor(scenarioName string) (string, error) {
	question, ok := Questions[scenarioName]
	if !ok {
		return "", fmt.Errorf("no frozen question for scenario %q", scenarioName)
	}
	return question, nil
}

// leakedVocabulary is checked against every question. The first group would
// tell either path how to shape the SQL; the second is Metis's own vocabulary,
// which would signal to the model that a Metis-shaped answer is expected and
// make the question an instruction rather than a request.
//
// "from" and "where" are deliberately absent, though both are SQL keywords.
// They are also ordinary English prepositions -- "sorted from highest to
// lowest", "for statuses where revenue is zero" -- and banning them would flag
// the plainest way to say what a person wants. A word that appears in natural
// speech is not evidence of SQL shape; multi-word SQL constructs and
// function-call syntax are, because they do not occur in a spoken question.
var leakedVocabulary = []string{
	"select ", " join", "group by", "order by", "having ",
	"union", "cte", "subquery", "window function", "partition by", "sum(", "count(",
	"semantic graph", "semantic node", "source_aggregate", "grain", "metric ref",
	"compile", "ossie", "dimension ref",
}

// LeakedVocabulary reports the banned phrases a question contains.
func LeakedVocabulary(question string) []string {
	lowered := strings.ToLower(question)
	var found []string
	for _, phrase := range leakedVocabulary {
		if strings.Contains(lowered, phrase) {
			found = append(found, strings.TrimSpace(phrase))
		}
	}
	sort.Strings(found)
	return found
}
