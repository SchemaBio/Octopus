SELECT format('CREATE ROLE octopus LOGIN PASSWORD %L', :'octopus_password')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'octopus') \gexec
SELECT format('CREATE ROLE sepiida LOGIN PASSWORD %L', :'sepiida_password')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'sepiida') \gexec

SELECT 'CREATE DATABASE octopus OWNER octopus'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'octopus') \gexec
SELECT 'CREATE DATABASE sepiida OWNER sepiida'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'sepiida') \gexec
