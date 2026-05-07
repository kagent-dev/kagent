-- Normalize the feedback primary key to (id) only.
--
-- Fresh installs from 000001 already have PRIMARY KEY (id) (BIGSERIAL).
-- Installs that upgraded from the GORM-managed schema retained the legacy
-- composite PRIMARY KEY (id, user_id) created by AutoMigrate.
--
-- Both variants use the default constraint name `feedback_pkey`, so this
-- atomic drop+add converges both paths to the single-column PK without
-- changing column types or affecting sqlc-generated code (queries do not
-- reference the PK; ListFeedback filters on user_id via idx_feedback_user_id).
ALTER TABLE feedback
    DROP CONSTRAINT feedback_pkey,
    ADD CONSTRAINT feedback_pkey PRIMARY KEY (id);
