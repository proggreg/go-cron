#!/bin/sh
set -e

SWAGGER_UI_VERSION="v5.17.11"
SWAGGER_UI_DIR="swagger-ui"

# Download Swagger UI
curl -L -o swagger-ui.zip "https://github.com/swagger-api/swagger-ui/archive/refs/tags/${SWAGGER_UI_VERSION}.zip"
unzip -q swagger-ui.zip
rm -rf ${SWAGGER_UI_DIR}
mv swagger-ui-${SWAGGER_UI_VERSION#v}/dist ${SWAGGER_UI_DIR}
rm -rf swagger-ui-${SWAGGER_UI_VERSION#v} swagger-ui.zip

# Configure index.html to use local swagger.yaml
sed -i '' 's|url: ".*"|url: "swagger.yaml"|' ${SWAGGER_UI_DIR}/index.html

echo "Swagger UI set up in ./${SWAGGER_UI_DIR}/"
echo "You can now access it at http://localhost:8080/swagger/" 