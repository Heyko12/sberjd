# Сборка и использование:
CGO_ENABLED=0 go build -o release/jd main.go

cd release

./jd -filterBy "items.metadata" a.json b.json

./jd -filterBy "*.metadata" a.json b.json

./jd -filterBy "items" a.json b.json

./jd -filterBy "" a.json b.json