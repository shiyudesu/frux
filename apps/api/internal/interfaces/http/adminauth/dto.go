package interfaceshttpadminauth

type loginRequest struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

type principalResponse struct {
	UserID      int64    `json:"user_id"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}

type loginResponse struct {
	AccessToken      string            `json:"access_token"`
	TokenType        string            `json:"token_type"`
	ExpiresInSeconds int64             `json:"expires_in_seconds"`
	Principal        principalResponse `json:"principal"`
}
