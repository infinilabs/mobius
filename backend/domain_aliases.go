package main

// Transitional aliases for the Phase-6 decomposition (plan 6.1): the shared
// types now live in internal/domain so new packages (storage, service, http)
// can depend on them without importing main. Existing main-package code keeps
// using the unqualified names via these aliases; they disappear as call sites
// migrate into the extracted packages.

import "mobius/internal/domain"

type (
	Task        = domain.Task
	TaskComment = domain.TaskComment

	Employee      = domain.Employee
	EmployeeBrief = domain.EmployeeBrief
	EmployeeModel = domain.EmployeeModel
	EmployeeSkill = domain.EmployeeSkill

	Project            = domain.Project
	ProjectAsset       = domain.ProjectAsset
	CreateProjectInput = domain.CreateProjectInput

	Message             = domain.Message
	FileRef             = domain.FileRef
	Conversation        = domain.Conversation
	ConversationSummary = domain.ConversationSummary

	TaskInteraction = domain.TaskInteraction
	Skill           = domain.Skill
	TokenUsage      = domain.TokenUsage
)
