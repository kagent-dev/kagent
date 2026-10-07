-- +goose Up

-- The generation of a quiescence holder's fencing token. Every Pause, Suspend
-- and fencing Resume a holder sends takes the next value, so a holder that took
-- a claim over always outranks the holder it replaced, and Substrate refuses a
-- request of the replaced one that lands late.
CREATE SEQUENCE quiescence_fencing_generation;

-- +goose Down

DROP SEQUENCE quiescence_fencing_generation;
