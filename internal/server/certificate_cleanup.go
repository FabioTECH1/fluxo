package server

import (
	"context"
	"log"
	"time"

	"fluxo/internal/database"
)

const certificateCleanupInterval = 15 * time.Minute

func (s *Server) signalCertificateCleanup() {
	select {
	case s.certificateCleanupWake <- struct{}{}:
	default:
	}
}

func (s *Server) certificateCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(certificateCleanupInterval)
	defer ticker.Stop()
	s.processCertificateCleanups(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processCertificateCleanups(ctx)
		case <-s.certificateCleanupWake:
			s.processCertificateCleanups(ctx)
		}
	}
}

func (s *Server) processCertificateCleanups(ctx context.Context) {
	domainMutationMu.Lock()
	defer domainMutationMu.Unlock()

	items, err := database.PendingCertificateCleanups(100)
	if err != nil {
		log.Printf("Certificate cleanup: load pending records: %v", err)
		return
	}
	for _, item := range items {
		if ctx.Err() != nil {
			return
		}
		used, err := database.CertificateStorageHasLiveReferences(item.CertPath, item.KeyPath)
		if err != nil {
			_ = database.FailCertificateCleanup(item.ID, "inspect live references: "+err.Error())
			continue
		}
		if used {
			if err := database.CompleteCertificateCleanup(item.ID, "retained", "Certificate storage is still referenced by a live site."); err != nil {
				log.Printf("Certificate cleanup: mark retained record %d: %v", item.ID, err)
			}
			continue
		}

		cert := database.Certificate{
			ID: item.CertificateID, SiteID: item.FormerSiteID, Domain: item.Domain,
			Provider: item.Provider, CertPath: item.CertPath, KeyPath: item.KeyPath,
			SourceCertificateID: item.SourceCertificateID,
		}
		if err := cleanupCertificateStorage(ctx, cert); err != nil {
			if recordErr := database.FailCertificateCleanup(item.ID, err.Error()); recordErr != nil {
				log.Printf("Certificate cleanup: record failure for %d: %v", item.ID, recordErr)
			}
			continue
		}
		if err := database.CompleteCertificateCleanup(item.ID, "cleaned", ""); err != nil {
			log.Printf("Certificate cleanup: mark record %d complete: %v", item.ID, err)
		}
	}
}
