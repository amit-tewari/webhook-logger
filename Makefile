# .envrc [https://direnv.net/]
#
# +HELLO 			# any random string
# +HELM				# helm binary path, helm v3 needed
# +IMAGE_NAME		# container image name in container registry
# +RELEASE_NAME		# helm release name for test
# +TEST_NAMESPACE	# K8s namespace for test release

envTest:
	echo ${HELLO}
	${HELM} -n ${TEST_NAMESPACE} list

build:
	CGO_ENABLED=1 go build -o app main.go

imageBuild:
	rm -f app
	docker build -t test .

imageTagAndPush:
	docker tag test:latest ${IMAGE_NAME}
	docker push ${IMAGE_NAME}

helmTemplate:
	${HELM} -n ${TEST_NAMESPACE} template  ${RELEASE_NAME} helm/ | less

helmTemplateTest:
	${HELM} -n ${TEST_NAMESPACE} template  ${RELEASE_NAME} helm/  -f helm/values-test.yaml | less

helmInstallTest:
	${HELM} -n ${TEST_NAMESPACE} upgrade --install ${RELEASE_NAME} helm/  -f helm/values-test.yaml

helmDeleteTest:
	${HELM} -n ${TEST_NAMESPACE} delete ${RELEASE_NAME}
