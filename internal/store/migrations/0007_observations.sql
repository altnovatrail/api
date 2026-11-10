-- What a submission read keeps (R46; architecture 3.2): the observation of its work, from which
-- the same release of module-kit gives the same verdict again with no AI — the reading of the
-- answer slots that AI read, joined from the two readers — the options tapped are in the answer,
-- as the values of their slots (R05; § 10 of spec's contracts text). Neither reader's own
-- observation is kept, nor the crop.

-- +goose Up

ALTER TABLE submissions ADD COLUMN observation bytea;
