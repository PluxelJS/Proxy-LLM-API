-- +goose Up
CREATE TABLE resources (
    kind TEXT NOT NULL,
    id TEXT NOT NULL,
    document TEXT NOT NULL CHECK(json_valid(document)),
    PRIMARY KEY(kind, id)
);
-- +goose Down
DROP TABLE resources;
