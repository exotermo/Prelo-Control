package api

type dashboardLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type dashboardChallengeResponse struct {
	Challenge string `json:"challenge"`
	NextStep  string `json:"nextStep"`
}

type dashboardChallengeCodeRequest struct {
	Challenge string `json:"challenge"`
	Code      string `json:"code"`
}

type dashboardTotpSetupResponse struct {
	Secret     string `json:"secret"`
	OtpauthURI string `json:"otpauthUri"`
}

type dashboardSessionResponse struct {
	AccessToken   string   `json:"accessToken"`
	ExpiresIn     int      `json:"expiresIn"`
	RecoveryCodes []string `json:"recoveryCodes"`
}

type dashboardActivateRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type dashboardEmailRequest struct {
	Email string `json:"email"`
}

type dashboardInviteRequest struct {
	Email string `json:"email"`
}

type userInviteRequest struct {
	Email string `json:"email"`
	// Role defaults to OPERATOR when omitted — inviting a new ADMIN is an explicit choice, never
	// the implicit default (least privilege).
	Role *string `json:"role"`
}

type changeRoleRequest struct {
	Role string `json:"role"`
}

type dashboardUserResponse struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	Role        string  `json:"role"`
	ActivatedAt *string `json:"activatedAt"`
	TOTPEnabled bool    `json:"totpEnabled"`
	CreatedAt   string  `json:"createdAt"`
}
