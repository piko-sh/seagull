-- sgl.query(GetUser, one)
-- sgl.column(id, type: int8, go_type: "github.com/google/uuid.UUID")
SELECT id, email FROM users WHERE id = $1;
