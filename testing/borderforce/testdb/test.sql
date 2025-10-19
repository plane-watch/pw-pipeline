CREATE DATABASE atc_test WITH TEMPLATE = template0 ENCODING = 'UTF8' LOCALE = 'en_US.utf8';

ALTER DATABASE atc_test OWNER TO test;

\connect atc_test

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

CREATE TABLE public.feeder_muxes (
    id bigint NOT NULL,
    name character varying,
    latitude numeric(9,6),
    longitude numeric(9,6),
    output_port integer,
    created_at timestamp(6) without time zone NOT NULL,
    updated_at timestamp(6) without time zone NOT NULL
);

ALTER TABLE public.feeder_muxes OWNER TO test;

CREATE SEQUENCE public.feeder_muxes_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER TABLE public.feeder_muxes_id_seq OWNER TO test;

ALTER SEQUENCE public.feeder_muxes_id_seq OWNED BY public.feeder_muxes.id;

ALTER TABLE ONLY public.feeder_muxes ALTER COLUMN id SET DEFAULT nextval('public.feeder_muxes_id_seq'::regclass);

INSERT INTO public.feeder_muxes VALUES (1, 'ACT', -35.308550, 149.124543, 12601, '2021-02-28 14:15:07.975475', '2021-02-28 14:15:07.975475');
INSERT INTO public.feeder_muxes VALUES (2, 'NSW', -33.857675, 151.214811, 12001, '2021-02-28 14:15:07.979973', '2021-02-28 14:15:07.979973');
INSERT INTO public.feeder_muxes VALUES (3, 'NT', -12.414492, 130.878375, 10801, '2021-02-28 14:15:07.983099', '2021-02-28 14:15:07.983099');
INSERT INTO public.feeder_muxes VALUES (4, 'QLD', -27.469628, 153.025063, 14001, '2021-02-28 14:15:07.986235', '2021-02-28 14:15:07.986235');
INSERT INTO public.feeder_muxes VALUES (5, 'SA', -34.928300, 138.600791, 15001, '2021-02-28 14:15:07.989114', '2021-02-28 14:15:07.989114');
INSERT INTO public.feeder_muxes VALUES (6, 'TAS', -42.882750, 147.327320, 17001, '2021-02-28 14:15:07.992168', '2021-02-28 14:15:07.992168');
INSERT INTO public.feeder_muxes VALUES (7, 'VIC', -37.668444, 144.840549, 13001, '2021-02-28 14:15:07.995385', '2021-02-28 14:15:07.995385');
INSERT INTO public.feeder_muxes VALUES (8, 'WA', -31.941679, 115.964458, 16001, '2021-02-28 14:15:07.998675', '2021-02-28 14:15:07.998675');
INSERT INTO public.feeder_muxes VALUES (9, 'NZ', -41.325240, 174.807248, 19001, '2021-02-28 14:15:08.001353', '2021-02-28 14:15:08.001353');
INSERT INTO public.feeder_muxes VALUES (10, 'mapwithlove', NULL, NULL, 21001, '2021-02-28 14:15:08.003967', '2021-02-28 14:15:08.003967');
INSERT INTO public.feeder_muxes VALUES (11, 'EU', 51.543440, 18.561281, 22001, '2021-04-14 15:29:04.504799', '2021-04-14 15:32:08.392421');
INSERT INTO public.feeder_muxes VALUES (12, 'US', 39.457298, -101.411172, 23001, '2021-04-14 15:33:31.80587', '2021-04-14 15:33:31.80587');
INSERT INTO public.feeder_muxes VALUES (13, 'Asia', 40.195899, 76.692354, 24001, '2022-02-03 06:40:03.06803', '2022-02-03 06:40:03.06803');

SELECT pg_catalog.setval('public.feeder_muxes_id_seq', 13, true);

ALTER TABLE ONLY public.feeder_muxes
    ADD CONSTRAINT feeder_muxes_pkey PRIMARY KEY (id);

CREATE TABLE public.feeders (
    id bigint NOT NULL,
    user_id integer,
    address character varying,
    mlat_enabled boolean DEFAULT true,
    latitude numeric(9,6),
    longitude numeric(9,6),
    feed_direction integer DEFAULT 0,
    feed_protocol integer DEFAULT 0,
    last_seen timestamp without time zone,
    api_key character varying,
    created_at timestamp(6) without time zone NOT NULL,
    updated_at timestamp(6) without time zone NOT NULL,
    mast_height numeric DEFAULT 0.0,
    altitude numeric DEFAULT 0.0,
    feed_host character varying,
    feed_port integer,
    feeder_mux_id integer,
    label character varying,
    feeder_code character varying
);

ALTER TABLE public.feeders OWNER TO test;

CREATE SEQUENCE public.feeders_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER TABLE public.feeders_id_seq OWNER TO test;

ALTER SEQUENCE public.feeders_id_seq OWNED BY public.feeders.id;

ALTER TABLE ONLY public.feeders ALTER COLUMN id SET DEFAULT nextval('public.feeders_id_seq'::regclass);

INSERT INTO public.feeders VALUES (1, 1, 'test address', true, 33.33333, -111.11111, 0, 0, '2023-05-19 10:13:30', 'ad84bf99-f24b-4b4c-83e3-28bfc331f7ad', '2023-02-26 02:40:49.214306', '2023-05-19 10:13:30.186184', 0.0, 0.0, '', NULL, 8, 'TestFeeder', 'TEST-0001');

CREATE TABLE public.users (
    id bigint NOT NULL,
    email character varying DEFAULT ''::character varying NOT NULL,
    encrypted_password character varying DEFAULT ''::character varying NOT NULL,
    reset_password_token character varying,
    reset_password_sent_at timestamp without time zone,
    remember_created_at timestamp without time zone,
    first_name character varying,
    last_name character varying,
    discord_nickname character varying,
    created_at timestamp(6) without time zone NOT NULL,
    updated_at timestamp(6) without time zone NOT NULL,
    provider character varying,
    uid character varying
);

ALTER TABLE public.users OWNER TO test;

CREATE SEQUENCE public.users_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER TABLE public.users_id_seq OWNER TO test;

ALTER SEQUENCE public.users_id_seq OWNED BY public.users.id;

ALTER TABLE ONLY public.users ALTER COLUMN id SET DEFAULT nextval('public.users_id_seq'::regclass);

INSERT INTO public.users VALUES (1, 'testuser@testing.test', 'supersecretpassword', NULL, NULL, NULL, 'Test', 'User', NULL, '2021-02-28 14:18:45.494264', '2021-02-28 14:18:45.494264', NULL, NULL);

SELECT pg_catalog.setval('public.feeder_muxes_id_seq', 13, true);
SELECT pg_catalog.setval('public.feeders_id_seq', 2, true);
SELECT pg_catalog.setval('public.users_id_seq', 2, true);
