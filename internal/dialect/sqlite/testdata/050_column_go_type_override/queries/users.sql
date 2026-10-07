-- sgl.query(GetUserUUID, one)
-- sgl.column(id, go_type: "github.com/google/uuid.UUID")
SELECT id, email FROM users WHERE id = ?;
