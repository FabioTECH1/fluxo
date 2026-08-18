package site

// quoteDotEnvValue preserves spaces and dotenv comment/interpolation markers.
// Site creation validation rejects single quotes for dotenv-based apps.
func quoteDotEnvValue(value string) string {
	return "'" + value + "'"
}
