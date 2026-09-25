ALTER TABLE gateway_audit_events
    ADD COLUMN requested_profile VARCHAR(100),
    ADD COLUMN attempt_order INT;
