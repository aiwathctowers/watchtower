package db

import (
	"fmt"
	"strings"
	"time"
)

// InboxLearnedRule represents a learned rule for inbox item classification.
type InboxLearnedRule struct {
	ID            int64
	RuleType      string
	ScopeKey      string
	Weight        float64
	Source        string
	EvidenceCount int
	LastUpdated   string
}

// UpsertLearnedRule inserts or updates a learned rule unconditionally.
func (db *DB) UpsertLearnedRule(r InboxLearnedRule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		INSERT INTO inbox_learned_rules (rule_type, scope_key, weight, source, evidence_count, last_updated)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(rule_type, scope_key) DO UPDATE SET
			weight = excluded.weight,
			source = excluded.source,
			evidence_count = excluded.evidence_count,
			last_updated = excluded.last_updated
	`, r.RuleType, r.ScopeKey, r.Weight, r.Source, r.EvidenceCount, now)
	return err
}

// UpsertLearnedRuleImplicit refuses to overwrite a rule whose source='user_rule'.
func (db *DB) UpsertLearnedRuleImplicit(r InboxLearnedRule) error {
	existing, err := db.GetLearnedRule(r.RuleType, r.ScopeKey)
	if err == nil && existing.Source == "user_rule" {
		return nil // protected
	}
	r.Source = "implicit"
	return db.UpsertLearnedRule(r)
}

// GetLearnedRule returns the rule matching the given type and scope key.
func (db *DB) GetLearnedRule(ruleType, scopeKey string) (InboxLearnedRule, error) {
	var r InboxLearnedRule
	err := db.QueryRow(`
		SELECT id, rule_type, scope_key, weight, source, evidence_count, last_updated
		FROM inbox_learned_rules WHERE rule_type=? AND scope_key=?
	`, ruleType, scopeKey).Scan(&r.ID, &r.RuleType, &r.ScopeKey, &r.Weight, &r.Source, &r.EvidenceCount, &r.LastUpdated)
	if err != nil {
		return r, fmt.Errorf("get learned rule: %w", err)
	}
	return r, nil
}

// ListAllLearnedRules returns all learned rules ordered by last_updated descending.
func (db *DB) ListAllLearnedRules() ([]InboxLearnedRule, error) {
	rows, err := db.Query(`SELECT id, rule_type, scope_key, weight, source, evidence_count, last_updated FROM inbox_learned_rules ORDER BY last_updated DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InboxLearnedRule
	for rows.Next() {
		var r InboxLearnedRule
		if err := rows.Scan(&r.ID, &r.RuleType, &r.ScopeKey, &r.Weight, &r.Source, &r.EvidenceCount, &r.LastUpdated); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListLearnedRulesByScope returns rules whose scope_key is in scopeKeys, up to limit rows,
// ordered by absolute weight descending.
func (db *DB) ListLearnedRulesByScope(scopeKeys []string, limit int) ([]InboxLearnedRule, error) {
	if len(scopeKeys) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(scopeKeys))
	args := make([]interface{}, len(scopeKeys)+1)
	for i, k := range scopeKeys {
		placeholders[i] = "?"
		args[i] = k
	}
	args[len(scopeKeys)] = limit
	q := fmt.Sprintf(`
		SELECT id, rule_type, scope_key, weight, source, evidence_count, last_updated
		FROM inbox_learned_rules
		WHERE scope_key IN (%s)
		ORDER BY ABS(weight) DESC, last_updated DESC
		LIMIT ?
	`, strings.Join(placeholders, ","))
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InboxLearnedRule
	for rows.Next() {
		var r InboxLearnedRule
		if err := rows.Scan(&r.ID, &r.RuleType, &r.ScopeKey, &r.Weight, &r.Source, &r.EvidenceCount, &r.LastUpdated); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteLearnedRule removes a learned rule by type and scope key.
func (db *DB) DeleteLearnedRule(ruleType, scopeKey string) error {
	_, err := db.Exec(`DELETE FROM inbox_learned_rules WHERE rule_type=? AND scope_key=?`, ruleType, scopeKey)
	return err
}
