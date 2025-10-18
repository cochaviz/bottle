package services

import (
	"cochaviz/mime/internal/models"
)

// LeaseService provides methods for managing leases.
type LeaseService interface {
	// Add fields here
	Acquire() (models.Lease, error)
}

// LeaseResolver resolves LeaseRequests to concrete LeaseSpecification objects.
type LeaseResolver interface {
	// Add fields here
}
