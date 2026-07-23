package server

func (s *Server) beginCertificateIssuance(siteID int) bool {
	s.certificateOperationMu.Lock()
	defer s.certificateOperationMu.Unlock()
	if s.certificateIssuances == nil {
		s.certificateIssuances = make(map[int]int)
	}
	if s.certificateSiteDeletions == nil {
		s.certificateSiteDeletions = make(map[int]bool)
	}
	if s.certificateSiteDeletions[siteID] {
		return false
	}
	s.certificateIssuances[siteID]++
	return true
}

func (s *Server) endCertificateIssuance(siteID int) {
	s.certificateOperationMu.Lock()
	defer s.certificateOperationMu.Unlock()
	if s.certificateIssuances[siteID] <= 1 {
		delete(s.certificateIssuances, siteID)
		return
	}
	s.certificateIssuances[siteID]--
}

func (s *Server) beginCertificateSiteDeletion(siteID int) bool {
	s.certificateOperationMu.Lock()
	defer s.certificateOperationMu.Unlock()
	if s.certificateIssuances == nil {
		s.certificateIssuances = make(map[int]int)
	}
	if s.certificateSiteDeletions == nil {
		s.certificateSiteDeletions = make(map[int]bool)
	}
	if s.certificateSiteDeletions[siteID] || s.certificateIssuances[siteID] > 0 {
		return false
	}
	s.certificateSiteDeletions[siteID] = true
	return true
}

func (s *Server) endCertificateSiteDeletion(siteID int) {
	s.certificateOperationMu.Lock()
	delete(s.certificateSiteDeletions, siteID)
	s.certificateOperationMu.Unlock()
}
