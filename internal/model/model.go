package model

// AgentDeployment mirrors hycert-api AgentDeploymentDTO.
type AgentDeployment struct {
	ID              uint   `json:"id"`
	CertificateID   uint   `json:"certificate_id"`
	TargetHost      string `json:"target_host"`
	TargetService   string `json:"target_service"`
	TargetDetail    string `json:"target_detail"`
	Port            *int   `json:"port"`
	DeployStatus    string `json:"deploy_status"`
	LastFingerprint string `json:"last_fingerprint"`
	CertFingerprint string `json:"cert_fingerprint"`
	AgentID         string `json:"agent_id,omitempty"`
}

// TargetDetail is the parsed target_detail JSON for deployment services.
type TargetDetail struct {
	OS        string `json:"os"`
	CertPath  string `json:"cert_path"`
	KeyPath   string `json:"key_path"`
	Password  string `json:"password,omitempty"` // JKS/PFX keystore password
	Alias     string `json:"alias,omitempty"`    // JKS key alias
	ReloadCmd string `json:"reload_cmd"`

	// ReloadTimeout overrides the per-service default reload timeout (seconds).
	// 0 = use service-type default from executor.ReloadTimeoutFor.
	ReloadTimeout int `json:"reload_timeout,omitempty"`

	// Post-deploy TLS fingerprint verification (see internal/verify/tls.go).
	// When SkipVerify is false (default), agent probes VerifyHost:VerifyPort
	// after reload and expects the presented cert fingerprint to match.
	SkipVerify   bool   `json:"skip_verify,omitempty"`
	VerifyHost   string `json:"verify_host,omitempty"`    // default: 127.0.0.1
	VerifyPort   int    `json:"verify_port,omitempty"`    // default: 443
	VerifyTimeoutSeconds int `json:"verify_timeout,omitempty"` // default: service-type dependent
}

// UpdateStatusRequest mirrors hycert-api UpdateDeployStatusRequest.
type UpdateStatusRequest struct {
	Action       string `json:"action"`
	Status       string `json:"status"`
	Fingerprint  string `json:"fingerprint,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	DurationMs   *int   `json:"duration_ms,omitempty"`
}

// DownloadResult represents the download API response data.
type DownloadResult struct {
	Format        string `json:"format"`
	Content       string `json:"content"`
	ContentBase64 string `json:"content_base64"`
	Filename      string `json:"filename"`
}

// APIResponse is the standard hycert-api envelope.
type APIResponse[T any] struct {
	Success bool   `json:"success"`
	Data    T      `json:"data,omitempty"`
	Error   *APIError `json:"error,omitempty"`
}

// APIError is the error object inside the envelope.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RegisterRequest is sent on agent startup to register with the server.
type RegisterRequest struct {
	AgentID     string   `json:"agent_id"`
	Name        string   `json:"name"`
	Hostname    string   `json:"hostname"`
	IPAddresses []string `json:"ip_addresses,omitempty"`
	OS          string   `json:"os,omitempty"`
	Version     string   `json:"version,omitempty"`
	Interval    int      `json:"interval,omitempty"`
}

// RegisterResponse is the server's response to a registration request.
type RegisterResponse struct {
	ID      uint   `json:"id"`
	AgentID string `json:"agent_id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
}
