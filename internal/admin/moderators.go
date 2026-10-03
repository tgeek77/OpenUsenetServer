package admin

import (
	"encoding/json"
	"net/http"

	"openusenet/internal/moderate"
	"openusenet/internal/store"
)

func (p *Portal) moderatorsAPI(w http.ResponseWriter, r *http.Request, _ store.User) {
	switch r.Method {
	case http.MethodGet:
		rules, err := p.st.ListModeratorRules(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		if rules == nil {
			rules = []store.ModeratorRule{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"rules":   rules,
			"default": moderate.DefaultAddress,
		})
	case http.MethodPost:
		var in struct {
			Pattern string `json:"pattern"`
			Address string `json:"address"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := p.st.AddModeratorRule(r.Context(), in.Pattern, in.Address); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "added"})
	case http.MethodDelete:
		pattern := r.URL.Query().Get("pattern")
		if err := p.st.DeleteModeratorRule(r.Context(), pattern); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": pattern})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
