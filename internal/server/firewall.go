package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	"fluxo/internal/services/firewall"
)

var firewallMutationMu sync.Mutex

// handleListFirewallRules returns Fluxo-managed records plus read-only rules
// discovered directly from UFW's persisted configuration.
func (s *Server) handleListFirewallRules() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		addedOutput, err := firewall.AddedRules()
		if err != nil {
			http.Error(w, "Failed to inspect UFW rule state: "+err.Error(), http.StatusInternalServerError)
			return
		}
		addedRules := firewall.ParseAddedRules(addedOutput)
		rows, err := database.DB.Query("SELECT id, name, rule_type, port, from_ip, managed_by, created_at FROM firewall_rules ORDER BY created_at DESC")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		managedRules := make([]database.FirewallRule, 0)
		for rows.Next() {
			var rule database.FirewallRule
			if err := rows.Scan(&rule.ID, &rule.Name, &rule.RuleType, &rule.Port, &rule.FromIP, &rule.ManagedBy, &rule.CreatedAt); err != nil {
				http.Error(w, "Failed to read Fluxo-managed firewall rules: "+err.Error(), http.StatusInternalServerError)
				return
			}
			managedRules = append(managedRules, rule)
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "Failed while reading Fluxo-managed firewall rules: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mergeFirewallRules(managedRules, addedRules))
	}
}

func mergeFirewallRules(managed []database.FirewallRule, added []firewall.AddedRule) []database.FirewallRule {
	result := make([]database.FirewallRule, 0, len(managed)+len(added))
	consumed := make([]bool, len(added))
	for _, managedRule := range managed {
		managedRule.Active = false
		for index, addedRule := range added {
			if !addedRule.Matches(managedRule.Port, managedRule.FromIP, managedRule.RuleType) {
				continue
			}
			managedRule.Active = true
			if !consumed[index] {
				consumed[index] = true
				break
			}
		}
		result = append(result, managedRule)
	}
	for index, addedRule := range added {
		if consumed[index] {
			continue
		}
		result = append(result, database.FirewallRule{
			ID:         -(index + 1),
			Name:       "External UFW rule",
			RuleType:   addedRule.RuleType,
			Port:       addedRule.Port,
			FromIP:     addedRule.FromIP,
			ManagedBy:  "external",
			Active:     true,
			RawCommand: addedRule.Command,
		})
	}
	return result
}

// handleCreateFirewallRule adds a UFW rule and persists it to the database.
func (s *Server) handleCreateFirewallRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name   string `json:"name"`
			Type   string `json:"type"`
			Port   string `json:"port"`
			FromIP string `json:"from_ip"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		req.Type = strings.ToLower(strings.TrimSpace(req.Type))
		req.Port = firewall.NormalizePort(req.Port)
		req.FromIP = firewall.NormalizeSource(req.FromIP)

		if req.Type == "" {
			req.Type = "allow"
		}
		if req.FromIP == "" {
			req.FromIP = "Any"
		}

		if req.Name == "" || len(req.Name) > 80 || safeinput.HasControlChars(req.Name) {
			http.Error(w, "Name is required, must be at most 80 characters, and cannot contain control characters", http.StatusBadRequest)
			return
		}
		if !safeinput.ValidateFirewallAction(req.Type) ||
			!safeinput.ValidateFirewallPortSpec(req.Port) ||
			!safeinput.ValidateFirewallSource(req.FromIP) {
			http.Error(w, "Invalid firewall action, port, or source", http.StatusBadRequest)
			return
		}
		firewallMutationMu.Lock()
		defer firewallMutationMu.Unlock()

		var existingID int
		err := database.DB.QueryRow("SELECT id FROM firewall_rules WHERE rule_type = ? AND port = ? AND from_ip = ? LIMIT 1", req.Type, req.Port, req.FromIP).Scan(&existingID)
		if err == nil {
			http.Error(w, "This firewall rule is already managed by Fluxo", http.StatusConflict)
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Failed to inspect existing firewall rules: "+err.Error(), http.StatusInternalServerError)
			return
		}
		addedRules, err := firewall.AddedRules()
		if err != nil {
			http.Error(w, "Failed to inspect UFW rule state: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if firewall.RuleExists(addedRules, req.Port, req.FromIP, req.Type) {
			http.Error(w, "An equivalent UFW rule already exists outside Fluxo; remove it manually before creating a dashboard-managed rule", http.StatusConflict)
			return
		}

		if err := firewall.AddRule(req.Port, req.FromIP, req.Type); err != nil {
			http.Error(w, "Failed to add firewall rule: "+err.Error(), http.StatusInternalServerError)
			return
		}

		res, err := database.DB.Exec("INSERT INTO firewall_rules (name, rule_type, port, from_ip, managed_by) VALUES (?, ?, ?, ?, 'dashboard')", req.Name, req.Type, req.Port, req.FromIP)
		if err != nil {
			if cleanupErr := firewall.DeleteRule(req.Port, req.FromIP, req.Type); cleanupErr != nil {
				http.Error(w, "The UFW rule was added but its Fluxo record and automatic cleanup both failed: "+err.Error()+"; cleanup: "+cleanupErr.Error(), http.StatusInternalServerError)
				return
			}
			http.Error(w, "The Fluxo record could not be saved, so the new UFW rule was rolled back: "+err.Error(), http.StatusInternalServerError)
			return
		}
		id, _ := res.LastInsertId()

		var rule database.FirewallRule
		if err := database.DB.QueryRow("SELECT id, name, rule_type, port, from_ip, managed_by, created_at FROM firewall_rules WHERE id = ?", id).Scan(&rule.ID, &rule.Name, &rule.RuleType, &rule.Port, &rule.FromIP, &rule.ManagedBy, &rule.CreatedAt); err != nil {
			http.Error(w, "Firewall rule was created but its Fluxo record could not be read: "+err.Error(), http.StatusInternalServerError)
			return
		}
		rule.Active = true

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rule)
	}
}

// handleDeleteFirewallRule removes a UFW rule and its database record.
func (s *Server) handleDeleteFirewallRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}
		firewallMutationMu.Lock()
		defer firewallMutationMu.Unlock()

		var port, fromIP, ruleType, managedBy string
		if err := database.DB.QueryRow("SELECT port, from_ip, rule_type, managed_by FROM firewall_rules WHERE id = ?", id).Scan(&port, &fromIP, &ruleType, &managedBy); err != nil {
			http.Error(w, "Rule not found", http.StatusNotFound)
			return
		}
		if managedBy == "installer" {
			http.Error(w, "Installer-managed firewall rules cannot be deleted from the dashboard", http.StatusConflict)
			return
		}

		addedRules, err := firewall.AddedRules()
		if err != nil {
			http.Error(w, "Failed to inspect UFW rule state: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if firewall.RuleExists(addedRules, port, fromIP, ruleType) {
			if err := firewall.DeleteRule(port, fromIP, ruleType); err != nil {
				http.Error(w, "Failed to remove firewall rule: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if _, err := database.DB.Exec("DELETE FROM firewall_rules WHERE id = ?", id); err != nil {
			http.Error(w, "Firewall rule was removed from UFW but its Fluxo record could not be removed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}
}
