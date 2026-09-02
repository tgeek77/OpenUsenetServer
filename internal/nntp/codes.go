package nntp

// RFC 3977 (and extension) response codes.
const (
	InfoHelp         = 100
	InfoCapabilities = 101
	InfoDate         = 111

	OKBannerPost   = 200
	OKBannerNoPost = 201
	OKQuit         = 205
	OKGroup        = 211
	OKList         = 215
	OKArticle      = 220
	OKHead         = 221
	OKBody         = 222
	OKStat         = 223
	OKOver         = 224
	OKHdr          = 225
	OKNewNews      = 230
	OKNewGroups    = 231
	OKIHave        = 235
	OKPost         = 240
	OKAuth         = 281

	ContIHave    = 335
	ContPost     = 340
	ContAuthPass = 381

	FailTerminating     = 400
	FailWrongMode       = 401
	FailAction          = 403
	FailBadGroup        = 411
	FailNoGroup         = 412
	FailArtnumInvalid   = 420
	FailNext            = 421
	FailPrev            = 422
	FailArtnumNotFound  = 423
	FailMsgidNotFound   = 430
	FailIHaveRefuse     = 435
	FailIHaveDefer      = 436
	FailIHaveReject     = 437
	FailPostAuth        = 440
	FailPostReject      = 441
	FailAuthNeeded      = 480
	FailPrivacyNeeded   = 483

	ErrCommand     = 500
	ErrSyntax      = 501
	ErrAccess      = 502
	ErrUnavailable = 503
	ErrBase64      = 504
)

const (
	MaxCommand = 512
	MaxArg     = 497
	MaxMsgID   = 250
	MaxArtNum  = 2147483647
)

const Version = "0.1.0"
const Software = "OpenUsenetServer"
