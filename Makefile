.PHONY: proto
# Regenerates api/gen from api/proto (lint first). See api/proto/README.md.
proto:
	go generate ./api/proto
