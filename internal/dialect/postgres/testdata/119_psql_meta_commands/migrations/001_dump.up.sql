--
-- PostgreSQL database dump
--

\restrict cRe26dxyO3We839cJ3COtDCpNvQBSEG33rrGwRIgsYLdkpPRu7RhWGxQhG36png

-- Dumped from database version 18.0
-- Dumped by pg_dump version 18.0

SET statement_timeout = 0;
SET client_encoding = 'UTF8';
SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE public.items (
    id bigint NOT NULL,
    name text NOT NULL
);

ALTER TABLE ONLY public.items
    ADD CONSTRAINT items_pkey PRIMARY KEY (id); \connect -reuse-previous=on "dbname='app'"

\unrestrict cRe26dxyO3We839cJ3COtDCpNvQBSEG33rrGwRIgsYLdkpPRu7RhWGxQhG36png
