package main

// Transitional aliases (plan 6.4c): the Elasticsearch layer lives in
// internal/search.

import (
	"mobius/internal/domain"
	"mobius/internal/search"
)

type (
	ESClient        = search.Client
	EmployeeMemory  = search.EmployeeMemory
	CreativeFilters = search.CreativeFilters
	SearchResult    = search.SearchResult
	Prompt          = domain.Prompt
)

const (
	IdxSkills           = search.IdxSkills
	IdxPrompts          = search.IdxPrompts
	IdxConversations    = search.IdxConversations
	IdxMessages         = search.IdxMessages
	IdxEmployeeMemories = search.IdxEmployeeMemories
	IdxProjectAssets    = search.IdxProjectAssets
	IdxEvents           = search.IdxEvents
	IdxEmployees        = search.IdxEmployees
	IdxProjects         = search.IdxProjects
	IdxTasks            = search.IdxTasks
)

func NewESClient(url string) (*ESClient, error) { return search.New(url) }
