package webui

import (
	"sort"
	"time"

	"github.com/hostpack/hostpack/internal/config"
	hpruntime "github.com/hostpack/hostpack/internal/runtime"
)

type GuestPack struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Hostname    string `json:"hostname"`
	Provider    string `json:"provider"`
	Java        int    `json:"java"`
}

type GuestCatalog struct {
	Packs []GuestPack `json:"packs"`
}

type GuestStatus struct {
	Phase       hpruntime.Phase `json:"phase"`
	ActiveID    string          `json:"activeId,omitempty"`
	ActiveName  string          `json:"activeName,omitempty"`
	StateChange time.Time       `json:"stateChange,omitempty"`
}

func Catalog(cfg *config.Config) GuestCatalog {
	ids := make([]string, 0, len(cfg.Packs))
	for id := range cfg.Packs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := GuestCatalog{Packs: make([]GuestPack, 0, len(ids))}
	for _, id := range ids {
		pack := cfg.Packs[id]
		result.Packs = append(result.Packs, GuestPack{ID: id, DisplayName: pack.DisplayName, Hostname: id + "." + cfg.Domain, Provider: pack.Provider, Java: pack.Java})
	}
	return result
}

func PublicStatus(cfg *config.Config, state hpruntime.State) GuestStatus {
	result := GuestStatus{Phase: state.Phase, ActiveID: state.ActiveID, StateChange: state.UpdatedAt}
	if pack, ok := cfg.Packs[state.ActiveID]; ok {
		result.ActiveName = pack.DisplayName
	}
	return result
}
