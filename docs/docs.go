// Package docs — placeholder для сгенерированной OpenAPI-спеки.
// Полноценный файл создаётся командой `swag init` (см. Makefile / Dockerfile).
package docs

import "github.com/swaggo/swag"

const docTemplate = `{
    "swagger": "2.0",
    "info": {
        "title": "Skinport Backend",
        "version": "1.0",
        "description": "Routes are populated by swag init from handler annotations."
    },
    "basePath": "/",
    "paths": {}
}`

var SwaggerInfo = &swag.Spec{
	Version:          "1.0",
	Host:             "",
	BasePath:         "/",
	Schemes:          []string{"http"},
	Title:            "Skinport Backend",
	Description:      "Skinport items + balance debit",
	InfoInstanceName: "swagger",
	SwaggerTemplate:  docTemplate,
}

func init() {
	swag.Register(SwaggerInfo.InstanceName(), SwaggerInfo)
}
