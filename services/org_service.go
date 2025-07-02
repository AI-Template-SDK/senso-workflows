// services/org_service.go
package services

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	workflowModels "github.com/AI-Template-SDK/senso-workflows/internal/models"
	"github.com/google/uuid"
)

type orgService struct {
	cfg        *config.Config
	httpClient *http.Client
	repos      *RepositoryManager
}

func NewOrgService(cfg *config.Config, repos *RepositoryManager) OrgService {
	return &orgService{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		repos: repos,
	}
}

func (s *orgService) GetOrgDetails(ctx context.Context, orgID string) (*RealOrgDetails, error) {
	fmt.Printf("[GetOrgDetails] Fetching real details for org: %s\n", orgID)

	// Parse orgID to UUID
	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		return nil, fmt.Errorf("invalid org ID format: %w", err)
	}

	// 1. Get organization from database
	org, err := s.repos.OrgRepo.GetByID(ctx, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get organization: %w", err)
	}
	if org == nil {
		return nil, fmt.Errorf("organization not found: %s", orgID)
	}

	// 2. Get geo models for this org
	models, err := s.repos.GeoModelRepo.GetByOrg(ctx, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get geo models: %w", err)
	}

	// 3. Get org locations
	locations, err := s.repos.OrgLocationRepo.GetByOrg(ctx, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get org locations: %w", err)
	}

	// 4. Get geo questions with tags
	questions, err := s.repos.GeoQuestionRepo.GetByOrgWithTags(ctx, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get geo questions: %w", err)
	}

	// 5. Get geo profiles to determine target company
	profiles, err := s.repos.GeoProfileRepo.GetByOrg(ctx, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get geo profiles: %w", err)
	}

	// 6. Get org websites for citation classification
	websites, err := s.repos.OrgWebsiteRepo.GetByOrg(ctx, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get org websites: %w", err)
	}

	// Extract just the URLs from website objects
	websiteURLs := make([]string, 0, len(websites))
	for _, website := range websites {
		websiteURLs = append(websiteURLs, website.URL)
	}

	// Determine target company from the first profile's company description
	targetCompany := org.Name // Default to org name
	if len(profiles) > 0 && profiles[0].CompanyDescription != nil {
		targetCompany = *profiles[0].CompanyDescription
	}

	orgDetails := &RealOrgDetails{
		Org:           org,
		Models:        models,
		Locations:     locations,
		Questions:     questions,
		TargetCompany: targetCompany,
		Profiles:      profiles,
		Websites:      websiteURLs,
	}

	fmt.Printf("[GetOrgDetails] Successfully loaded org: %s with %d models, %d locations, %d questions, %d websites, target: %s\n",
		org.Name, len(models), len(locations), len(questions), len(websiteURLs), targetCompany)

	return orgDetails, nil
}

func (s *orgService) GetOrgsByCreationWeekday(ctx context.Context, weekday time.Weekday) ([]*workflowModels.OrgSummary, error) {
	// Get all organizations first - then filter by weekday
	orgs, err := s.repos.OrgRepo.List(ctx, 1000, 0) // Get up to 1000 orgs
	if err != nil {
		return nil, fmt.Errorf("failed to list organizations: %w", err)
	}

	var filteredOrgs []*workflowModels.OrgSummary
	for _, org := range orgs {
		// Check if org was created on the target weekday
		if org.CreatedAt.Weekday() == weekday {
			summary := &workflowModels.OrgSummary{
				ID:        org.OrgID,
				Name:      org.Name,
				CreatedAt: org.CreatedAt,
				IsActive:  org.DeletedAt == nil, // Active if not deleted
			}
			filteredOrgs = append(filteredOrgs, summary)
		}
	}

	fmt.Printf("[GetOrgsByCreationWeekday] Found %d orgs created on %s\n", len(filteredOrgs), weekday.String())
	return filteredOrgs, nil
}

func (s *orgService) GetOrgsScheduledForDate(ctx context.Context, date time.Time) ([]string, error) {
	// Get organizations scheduled for this date (based on weekday)
	weekday := date.Weekday()
	orgs, err := s.GetOrgsByCreationWeekday(ctx, weekday)
	if err != nil {
		return nil, err
	}

	var orgIDs []string
	for _, org := range orgs {
		orgIDs = append(orgIDs, org.ID.String())
	}

	return orgIDs, nil
}

func (s *orgService) GetOrgCountByWeekday(ctx context.Context) (map[string]int, error) {
	// Get all organizations
	orgs, err := s.repos.OrgRepo.List(ctx, 10000, 0) // Get large number of orgs
	if err != nil {
		return nil, fmt.Errorf("failed to list organizations: %w", err)
	}

	// Count by weekday
	distribution := map[string]int{
		"Monday":    0,
		"Tuesday":   0,
		"Wednesday": 0,
		"Thursday":  0,
		"Friday":    0,
		"Saturday":  0,
		"Sunday":    0,
	}

	for _, org := range orgs {
		if org.DeletedAt == nil { // Only count active orgs
			weekday := org.CreatedAt.Weekday().String()
			distribution[weekday]++
		}
	}

	return distribution, nil
}
