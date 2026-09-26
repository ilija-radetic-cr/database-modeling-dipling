package llmpipeline

import (
	"fmt"
	"sort"
	"strings"

	"dbdsl/internal/dsl"
)

// The description contract is what the conceptual prompt promises about its
// output, checked mechanically:
//   - evidence and exclusions name existing segments, one ID per entry;
//   - a segment is either evidence or excluded, not both;
//   - every reference follows one grammar: a thing ID (or an actor, read as the
//     thing that represents it), or "thing.X" where X is a property of the
//     thing, a thing linked with it, or its state ("thing.stanje");
//   - a link is stated once, not from both sides;
//   - a word is written in one script.
//
// ConceptualDescriptionIssues lists violations so the model can correct them in
// its one feedback round. sanitizeConceptualDescription then repairs whatever
// is still left in a way that never invents structure: an unresolved reference
// is dropped, and an identity or uniqueness rule that would lose a part is
// dropped as a whole rather than turned into a different, weaker key.

const stateReference = "stanje"

type descriptionIndex struct {
	things     map[string]bool
	actorThing map[string]string
	properties map[string]map[string]bool
	linked     map[string]map[string]bool
	hasStates  map[string]bool
}

func newDescriptionIndex(d ConceptualDescription) descriptionIndex {
	ix := descriptionIndex{things: map[string]bool{}, actorThing: map[string]string{}, properties: map[string]map[string]bool{},
		linked: map[string]map[string]bool{}, hasStates: map[string]bool{}}
	for _, thing := range d.Things {
		id := strings.TrimSpace(thing.ID)
		ix.things[id] = true
		ix.properties[id] = map[string]bool{}
		ix.linked[id] = map[string]bool{}
		for _, property := range thing.Properties {
			ix.properties[id][snakeIdentifier(property.Name)] = true
		}
		ix.hasStates[id] = len(thing.States) > 0
	}
	for _, actor := range d.Actors {
		if ix.things[actor.RepresentedBy] {
			ix.actorThing[actor.ID] = actor.RepresentedBy
		}
	}
	for _, thing := range d.Things {
		for _, link := range thing.Links {
			if to := ix.thing(link.To); to != "" {
				ix.linked[thing.ID][to] = true
				ix.linked[to][thing.ID] = true
			}
		}
	}
	return ix
}

// thing resolves a thing ID, or an actor ID to the thing that represents it.
func (ix descriptionIndex) thing(id string) string {
	id = strings.TrimSpace(id)
	if ix.things[id] {
		return id
	}
	return ix.actorThing[id]
}

// member reports whether x names something of thing: a property, a linked
// thing or, when allowState is set, the thing's state.
func (ix descriptionIndex) member(thing, x string, allowState bool) bool {
	x = strings.TrimSpace(x)
	if ix.properties[thing][snakeIdentifier(x)] {
		return true
	}
	if other := ix.thing(x); other != "" && ix.linked[thing][other] {
		return true
	}
	return allowState && x == stateReference && ix.hasStates[thing]
}

// resolves reports whether ref is a thing or "thing.X" per the grammar above.
func (ix descriptionIndex) resolves(ref string) bool {
	ref = strings.TrimSpace(ref)
	head, tail, dotted := strings.Cut(ref, ".")
	thing := ix.thing(head)
	if thing == "" {
		return false
	}
	return !dotted || ix.member(thing, tail, true)
}

// identifies reports whether ref can be part of thing's identity: one of its
// properties or a thing it is linked with, optionally written as "thing.X".
func (ix descriptionIndex) identifies(thing, ref string) bool {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), thing+".")
	return ix.member(thing, ref, false)
}

// referenceLists enumerates every reference list of the description with a
// label for messages and a pointer for repairs.
func referenceLists(d *ConceptualDescription) []struct {
	label string
	refs  *[]string
} {
	var out []struct {
		label string
		refs  *[]string
	}
	add := func(label string, refs *[]string) {
		out = append(out, struct {
			label string
			refs  *[]string
		}{label, refs})
	}
	for i := range d.Rules {
		add(fmt.Sprintf("rule %s applies_to", d.Rules[i].ID), &d.Rules[i].AppliesTo)
	}
	for i := range d.Queries {
		add(fmt.Sprintf("query %s needs", d.Queries[i].ID), &d.Queries[i].Needs)
		add(fmt.Sprintf("query %s criteria", d.Queries[i].ID), &d.Queries[i].Criteria)
	}
	for i := range d.Imports {
		add(fmt.Sprintf("import %s fills", d.Imports[i].ID), &d.Imports[i].Fills)
	}
	for i := range d.OpenQuestions {
		add(fmt.Sprintf("open question %s affects", d.OpenQuestions[i].ID), &d.OpenQuestions[i].Affects)
	}
	return out
}

// allEvidence enumerates every evidence object with a label.
func allEvidence(d *ConceptualDescription) map[string]*DescriptionEvidence {
	out := map[string]*DescriptionEvidence{}
	for i := range d.Actors {
		out["actor "+d.Actors[i].ID] = &d.Actors[i].Evidence
	}
	for i := range d.Things {
		thing := &d.Things[i]
		out["thing "+thing.ID] = &thing.Evidence
		for p := range thing.Properties {
			out[fmt.Sprintf("property %s.%s", thing.ID, thing.Properties[p].Name)] = &thing.Properties[p].Evidence
		}
		for l := range thing.Links {
			out[fmt.Sprintf("link %s -> %s (#%d)", thing.ID, thing.Links[l].To, l+1)] = &thing.Links[l].Evidence
		}
		for t := range thing.Transitions {
			out[fmt.Sprintf("transition %s %s -> %s (#%d)", thing.ID, thing.Transitions[t].From, thing.Transitions[t].To, t+1)] = &thing.Transitions[t].Evidence
		}
	}
	for i := range d.Rules {
		out["rule "+d.Rules[i].ID] = &d.Rules[i].Evidence
	}
	for i := range d.Queries {
		out["query "+d.Queries[i].ID] = &d.Queries[i].Evidence
	}
	for i := range d.Imports {
		out["import "+d.Imports[i].ID] = &d.Imports[i].Evidence
	}
	for i := range d.Boundaries {
		out[fmt.Sprintf("boundary #%d", i+1)] = &d.Boundaries[i].Evidence
	}
	for i := range d.OpenQuestions {
		out["open question "+d.OpenQuestions[i].ID] = &d.OpenQuestions[i].Evidence
	}
	return out
}

// statedTwice returns the thing pairs that state one link from both sides.
// Links in opposite directions where each side refers to exactly one of the
// other are two facts (a request belongs to a user; the user points to one
// latest request) and are not reported.
func statedTwice(d ConceptualDescription) [][2]string {
	declared := map[[2]string]bool{}
	toOne := map[[2]string]bool{}
	for _, thing := range d.Things {
		for _, link := range thing.Links {
			pair := [2]string{thing.ID, strings.TrimSpace(link.To)}
			refersToOne := !countIsMany(link.PerThis, false) && countIsMany(link.PerOther, true)
			if !declared[pair] {
				toOne[pair] = refersToOne
			} else {
				toOne[pair] = toOne[pair] && refersToOne
			}
			declared[pair] = true
		}
	}
	var pairs [][2]string
	for pair := range declared {
		reverse := [2]string{pair[1], pair[0]}
		if pair[0] < pair[1] && declared[reverse] && !(toOne[pair] && toOne[reverse]) {
			pairs = append(pairs, pair)
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i][0]+pairs[i][1] < pairs[j][0]+pairs[j][1] })
	return pairs
}

// maxReportedIssues keeps the feedback prompt readable; the model is told how
// many more there are.
const maxReportedIssues = 40

// ConceptualDescriptionIssues lists contract violations for the feedback round.
// It does not modify the description.
func ConceptualDescriptionIssues(d ConceptualDescription, units []dsl.SourceUnit) []string {
	known := map[string]bool{}
	for _, unit := range units {
		known[unit.ID] = true
	}
	ix := newDescriptionIndex(d)
	var issues []string
	labels := []string{}
	evidence := allEvidence(&d)
	for label := range evidence {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	cited := map[string]bool{}
	for _, label := range labels {
		for _, id := range evidence[label].Segments {
			id = strings.TrimSpace(id)
			if !known[id] {
				issues = append(issues, fmt.Sprintf("%s cites %q, which is not a segment ID; cite each segment separately by its exact ID", label, id))
				continue
			}
			cited[id] = true
		}
	}
	for _, item := range d.Excluded {
		id := strings.TrimSpace(item.Segment)
		switch {
		case !known[id]:
			issues = append(issues, fmt.Sprintf("excluded lists %q, which is not a segment ID; list each segment separately by its exact ID", id))
		case cited[id]:
			issues = append(issues, fmt.Sprintf("segment %s is both excluded and cited as evidence; a segment that says something about the data is not excluded", id))
		}
	}
	for _, thing := range d.Things {
		for _, link := range thing.Links {
			if ix.thing(link.To) == "" {
				issues = append(issues, fmt.Sprintf("thing %s links to %q, which is not a thing ID", thing.ID, link.To))
			}
		}
		for _, ref := range thing.IdentifiedBy {
			if !ix.identifies(thing.ID, ref) {
				issues = append(issues, fmt.Sprintf("thing %s identified_by %q is neither a property of %s nor a thing linked with it", thing.ID, ref, thing.ID))
			}
		}
		if thing.InstanceOf != "" && !ix.things[thing.InstanceOf] {
			issues = append(issues, fmt.Sprintf("thing %s instance_of %q is not a thing ID", thing.ID, thing.InstanceOf))
		}
		for _, property := range thing.Properties {
			if property.Origin == "derived" && strings.TrimSpace(property.Source) == "" {
				issues = append(issues, fmt.Sprintf("property %s.%s is derived but its source does not say how it is computed", thing.ID, property.Name))
			}
		}
	}
	for _, actor := range d.Actors {
		if actor.RepresentedBy != "" && !ix.things[actor.RepresentedBy] {
			issues = append(issues, fmt.Sprintf("actor %s represented_by %q is not a thing ID", actor.ID, actor.RepresentedBy))
		}
	}
	for i, rule := range d.Rules {
		switch rule.Comparison {
		case "":
		case "case_sensitive", "case_insensitive":
			if rule.Kind != "uniqueness" {
				issues = append(issues, fmt.Sprintf("rule %s comparison is valid only for uniqueness rules", nonEmpty(rule.ID, fmt.Sprintf("[%d]", i))))
			}
		default:
			issues = append(issues, fmt.Sprintf("rule %s has invalid comparison %q", nonEmpty(rule.ID, fmt.Sprintf("[%d]", i)), rule.Comparison))
		}
	}
	for _, thing := range d.Things {
		declared := map[string]bool{}
		for _, state := range thing.States {
			state = strings.TrimSpace(state)
			switch {
			case state == "":
				issues = append(issues, fmt.Sprintf("thing %s has an empty state", thing.ID))
			case declared[state]:
				issues = append(issues, fmt.Sprintf("thing %s has duplicate state %q", thing.ID, state))
			}
			declared[state] = true
		}
		for i, transition := range thing.Transitions {
			for _, end := range []struct{ side, state string }{{"from", transition.From}, {"to", transition.To}} {
				if !declared[strings.TrimSpace(end.state)] {
					issues = append(issues, fmt.Sprintf("thing %s transition[%d] %s %q is not one declared state", thing.ID, i, end.side, end.state))
				}
			}
		}
	}
	for _, list := range referenceLists(&d) {
		for _, ref := range *list.refs {
			if !ix.resolves(ref) {
				issues = append(issues, fmt.Sprintf("%s %q does not resolve; use a thing ID or \"thing.X\" where X is a property of that thing, a thing linked with it, or \"%s\"", list.label, ref, stateReference))
			}
		}
	}
	for _, pair := range statedTwice(d) {
		issues = append(issues, fmt.Sprintf("the link between %s and %s is stated from both sides with counts that describe one link; state it once, on the thing that refers to the other", pair[0], pair[1]))
	}
	if len(issues) > maxReportedIssues {
		more := len(issues) - maxReportedIssues
		issues = append(issues[:maxReportedIssues], fmt.Sprintf("… and %d more issues of the same kinds", more))
	}
	return issues
}

// sanitizeConceptualDescription repairs what the feedback round left, and
// returns one warning per kind of repair.
func sanitizeConceptualDescription(d *ConceptualDescription, units []dsl.SourceUnit) []string {
	var warnings []string
	comparisonCleared := []string{}
	for i := range d.Rules {
		rule := &d.Rules[i]
		if rule.Comparison != "" && (rule.Kind != "uniqueness" || (rule.Comparison != "case_sensitive" && rule.Comparison != "case_insensitive")) {
			comparisonCleared = append(comparisonCleared, nonEmpty(rule.ID, fmt.Sprintf("[%d]", i)))
			rule.Comparison = ""
		}
	}
	if len(comparisonCleared) > 0 {
		warnings = append(warnings, "invalid comparison semantics were removed from rules: "+strings.Join(comparisonCleared, ", "))
	}
	// The description is written in ASCII Serbian; this also settles words the
	// model wrote half in Cyrillic, half in Latin.
	writeASCIISerbian(d)
	warnings = append(warnings, sanitizeStates(d)...)
	known := map[string]bool{}
	for _, unit := range units {
		known[unit.ID] = true
	}
	dropped := map[string]bool{}
	cited := map[string]bool{}
	for _, evidence := range allEvidence(d) {
		kept := []string{}
		for _, id := range evidence.Segments {
			id = strings.TrimSpace(id)
			if known[id] {
				kept = appendUnique(kept, id)
				cited[id] = true
			} else if id != "" {
				dropped[id] = true
			}
		}
		evidence.Segments = kept
	}
	excluded := []DescriptionExcluded{}
	unexcluded := []string{}
	for _, item := range d.Excluded {
		item.Segment = strings.TrimSpace(item.Segment)
		switch {
		case !known[item.Segment]:
			if item.Segment != "" {
				dropped[item.Segment] = true
			}
		case cited[item.Segment]:
			unexcluded = append(unexcluded, item.Segment)
		default:
			excluded = append(excluded, item)
		}
	}
	d.Excluded = excluded
	if len(dropped) > 0 {
		warnings = append(warnings, "unknown segment IDs were removed: "+strings.Join(sortedKeys(dropped), ", "))
	}
	if len(unexcluded) > 0 {
		warnings = append(warnings, "segments cited as evidence were removed from excluded: "+strings.Join(unexcluded, ", "))
	}

	ix := newDescriptionIndex(*d)
	for i := range d.Actors {
		if d.Actors[i].RepresentedBy != "" && !ix.things[d.Actors[i].RepresentedBy] {
			warnings = append(warnings, fmt.Sprintf("actor %s represented_by %q is not a thing; the reference was cleared", d.Actors[i].ID, d.Actors[i].RepresentedBy))
			d.Actors[i].RepresentedBy = ""
		}
	}
	ix = newDescriptionIndex(*d)
	for i := range d.Things {
		thing := &d.Things[i]
		if thing.InstanceOf != "" && !ix.things[thing.InstanceOf] {
			warnings = append(warnings, fmt.Sprintf("thing %s instance_of %q is not a thing; the reference was cleared", thing.ID, thing.InstanceOf))
			thing.InstanceOf = ""
		}
		links := []DescriptionLink{}
		for _, link := range thing.Links {
			if to := ix.thing(link.To); to != "" {
				link.To = to
				links = append(links, link)
			} else {
				warnings = append(warnings, fmt.Sprintf("thing %s link to %q was removed: it is not a thing", thing.ID, link.To))
			}
		}
		thing.Links = links
	}
	ix = newDescriptionIndex(*d)
	for i := range d.Things {
		thing := &d.Things[i]
		for _, ref := range thing.IdentifiedBy {
			if !ix.identifies(thing.ID, ref) {
				warnings = append(warnings, fmt.Sprintf("identity of %s was not kept: %q is neither its property nor a linked thing, and a partial identity would be a different key", thing.ID, ref))
				thing.IdentifiedBy = DescriptionRefs{}
				break
			}
		}
	}
	for i := range d.Rules {
		rule := &d.Rules[i]
		if rule.Kind != "uniqueness" {
			continue
		}
		for _, ref := range rule.AppliesTo {
			if !ix.resolves(ref) {
				warnings = append(warnings, fmt.Sprintf("uniqueness rule %s is kept only as text: %q does not resolve, and a partial key would be a different rule", rule.ID, ref))
				rule.AppliesTo = []string{}
				break
			}
		}
	}
	unresolved := map[string]bool{}
	for _, list := range referenceLists(d) {
		kept := []string{}
		for _, ref := range *list.refs {
			if ix.resolves(ref) {
				kept = append(kept, strings.TrimSpace(ref))
			} else {
				unresolved[ref] = true
			}
		}
		*list.refs = kept
	}
	if len(unresolved) > 0 {
		warnings = append(warnings, "unresolved references were removed: "+strings.Join(sortedKeys(unresolved), ", "))
	}
	return warnings
}

// sanitizeStates keeps each state once and drops a transition whose ends are not
// declared states: a lifecycle cannot move to a state it does not have, and the
// deterministic transformation would silently skip such a transition anyway.
func sanitizeStates(d *ConceptualDescription) []string {
	var warnings []string
	for i := range d.Things {
		thing := &d.Things[i]
		states := trimmedUniqueStrings(thing.States)
		if len(states) != len(thing.States) {
			warnings = append(warnings, fmt.Sprintf("thing %s: empty or repeated states were removed", thing.ID))
		}
		thing.States = states
		declared := map[string]bool{}
		for _, state := range states {
			declared[state] = true
		}
		kept := []DescriptionTransition{}
		for _, transition := range thing.Transitions {
			transition.From, transition.To = strings.TrimSpace(transition.From), strings.TrimSpace(transition.To)
			if declared[transition.From] && declared[transition.To] {
				kept = append(kept, transition)
				continue
			}
			warnings = append(warnings, fmt.Sprintf("thing %s: transition %q -> %q was removed because its ends are not declared states", thing.ID, transition.From, transition.To))
		}
		thing.Transitions = kept
	}
	return warnings
}
