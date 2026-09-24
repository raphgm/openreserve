module github.com/openreserve/apps/orpay/backend

go 1.26.5

require (
	github.com/SherClockHolmes/webpush-go v1.4.0
	github.com/openreserve/node v0.0.0
)

require (
	github.com/golang-jwt/jwt/v5 v5.2.1 // indirect
	golang.org/x/crypto v0.32.0 // indirect
)

replace github.com/openreserve/node => ../../../node
