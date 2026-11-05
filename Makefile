VERSION ?= dev
PROFILE ?= co2
REGION  ?= ap-northeast-1
THING   ?= co2-gg-core

COMPONENT = com.example.Co2Device
BUCKET    = co2-monitor-artifacts-$(ACCOUNT_ID)-$(REGION)-an
AWS       = aws --profile $(PROFILE) --region $(REGION)

.PHONY: test build release deploy status server check-version check-account

test:
	go test ./...

build:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o dist/co2-device ./cmd/device

release: check-version check-account build
	sed -e 's/<BUCKET>/$(BUCKET)/' -e 's/<VERSION>/$(VERSION)/g' deploy/recipe.yaml > dist/recipe.yaml
	$(AWS) s3 cp dist/co2-device s3://$(BUCKET)/co2-device/$(VERSION)/co2-device
	$(AWS) greengrassv2 create-component-version --inline-recipe fileb://dist/recipe.yaml
	sleep 5
	$(AWS) greengrassv2 describe-component \
		--arn arn:aws:greengrass:$(REGION):$(ACCOUNT_ID):components:$(COMPONENT):versions:$(VERSION) \
		--query status.componentState --output text

deploy: check-version check-account
	$(AWS) greengrassv2 create-deployment \
		--target-arn arn:aws:iot:$(REGION):$(ACCOUNT_ID):thing/$(THING) \
		--deployment-name co2-monitor \
		--components '{"$(COMPONENT)":{"componentVersion":"$(VERSION)","runWith":{"posixUser":"co2monitor:i2c"}}}'

status:
	$(AWS) greengrassv2 list-installed-components --core-device-thing-name $(THING)

# エンドポイントはアカウントごとに違うので、リポジトリに書かず毎回 AWS に問い合わせる
server:
	MQTT_BROKER=ssl://$$($(AWS) iot describe-endpoint --endpoint-type iot:Data-ATS --query endpointAddress --output text):8883 \
	MQTT_CA_FILE=certs/AmazonRootCA1.pem \
	MQTT_CERT_FILE=certs/server.cert.pem \
	MQTT_KEY_FILE=certs/server.private.key \
	go run ./cmd/server

check-version:
	@test "$(VERSION)" != dev || { echo "VERSION を指定してください（例: make release VERSION=1.0.2）"; exit 1; }

check-account:
	@test -n "$(ACCOUNT_ID)" || { echo "ACCOUNT_ID を設定してください（例: export ACCOUNT_ID=123456789012）"; exit 1; }
