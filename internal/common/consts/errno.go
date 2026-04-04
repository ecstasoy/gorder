package consts

const (
	ErrnoSuccess      = 0
	ErrnoUnknownError = 1

	ErrnoBindRequestError     = 1000
	ErrnoRequestValidateError = 1001
)

var ErrMsg = map[int]string{
	ErrnoSuccess:      "success",
	ErrnoUnknownError: "unknown error",

	ErrnoBindRequestError:     "binding request failed",
	ErrnoRequestValidateError: "request validation failed",
}
