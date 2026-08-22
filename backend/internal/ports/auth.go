package ports

// TokenIssuer emite y verifica los tokens JWT usados por la API. El dominio
// no depende de esto directamente; lo usa el adaptador HTTP.
type TokenIssuer interface {
	Issue(userID string) (string, error)
	Verify(token string) (userID string, err error)
}
