package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"fluxo/internal/database"
	"fluxo/internal/services/firewall"
)

// handleListFirewallRules returns all firewall rules.
func (s *Server) handleListFirewallRules() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.DB.Query("SELECT id, name, rule_type, port, from_ip, created_at FROM firewall_rules ORDER BY created_at DESC")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		rules := make([]database.FirewallRule, 0)
		for rows.Next() {
			var rule database.FirewallRule
			if err := rows.Scan(&rule.ID, &rule.Name, &rule.RuleType, &rule.Port, &rule.FromIP, &rule.CreatedAt); err != nil {
				continue
			}
			rules = append(rules, rule)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rules)
	}
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
		req.Type = strings.TrimSpace(req.Type)
		req.Port = strings.TrimSpace(req.Port)
		req.FromIP = strings.TrimSpace(req.FromIP)

		if req.Type == "" {
			req.Type = "allow"
		}
		if req.FromIP == "" {
			req.FromIP = "Any"
		}

		if req.Name == "" || req.Port == "" {
			http.Error(w, "Name and port are required", http.StatusBadRequest)
			return
		}

		if err := firewall.AddRule(req.Port, req.FromIP, req.Type); err != nil {
			http.Error(w, "Failed to add firewall rule: "+err.Error(), http.StatusInternalServerError)
			return
		}

		res, err := database.DB.Exec("INSERT INTO firewall_rules (name, rule_type, port, from_ip) VALUES (?, ?, ?, ?)", req.Name, req.Type, req.Port, req.FromIP)
		if err != nil {
			_ = firewall.DeleteRule(req.Port, req.FromIP, req.Type)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		id, _ := res.LastInsertId()

		var rule database.FirewallRule
		database.DB.QueryRow("SELECT id, name, rule_type, port, from_ip, created_at FROM firewall_rules WHERE id = ?", id).Scan(&rule.ID, &rule.Name, &rule.RuleType, &rule.Port, &rule.FromIP, &rule.CreatedAt)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rule)
	}
}

// handleDeleteFirewallRule removes a UFW rule and its database record.
func (s *Server) handleDeleteFirewallRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}

		var port, fromIP, ruleType string
		if err := database.DB.QueryRow("SELECT port, from_ip, rule_type FROM firewall_rules WHERE id = ?", id).Scan(&port, &fromIP, &ruleType); err != nil {
			http.Error(w, "Rule not found", http.StatusNotFound)
			return
		}

		if err := firewall.DeleteRule(port, fromIP, ruleType); err != nil {
			http.Error(w, "Failed to remove firewall rule: "+err.Error(), http.StatusInternalServerError)
			return
		}

		database.DB.Exec("DELETE FROM firewall_rules WHERE id = ?", id)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}
}
