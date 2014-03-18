CREATE TABLE files (
	file_id oid PRIMARY KEY DEFAULT lo_create(0),
	name text UNIQUE NOT NULL,
	size bigint,
	type text,
	digest text,
	created_at timestamp with time zone NOT NULL DEFAULT current_timestamp
);

CREATE OR REPLACE FUNCTION delete_file() RETURNS TRIGGER AS $$
    BEGIN
        PERFORM lo_unlink(OLD.file_id);
        RETURN NULL;
    END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS delete_files ON files;
CREATE TRIGGER delete_file
    AFTER DELETE ON files
    FOR EACH ROW EXECUTE PROCEDURE delete_file();
