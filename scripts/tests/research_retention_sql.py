# Generate isolated PostgreSQL regression fixtures. All mutations target
# pg_temp objects and the transaction ends with ROLLBACK. No production writes.
from pathlib import Path
import re,json
o=Path(__file__).resolve().parents[2]
ret=(o/'internal/database/retention.go').read_text(encoding='utf-8')
focus=(o/'internal/database/focus_migration.go').read_text(encoding='utf-8')
deadline=re.search(r'tx.Exec\(`(CREATE OR REPLACE FUNCTION research_retention_deadline.*?)`\)',ret,re.S).group(1)
fence=re.search(r'tx.Exec\(`(CREATE OR REPLACE FUNCTION research_task_content_fence.*?)`\)',ret,re.S).group(1)
transform=re.search(r'tx.Exec\(`(CREATE OR REPLACE FUNCTION research_migrate_focus_json.*?)`\)',focus,re.S).group(1)
sql='''BEGIN;
CREATE TEMP TABLE retention_test_namespace(dummy int);
SET LOCAL search_path TO pg_temp;
CREATE TABLE tasks(id text PRIMARY KEY,uuid text UNIQUE,created_at timestamptz NOT NULL,expires_at timestamptz,deleted_at timestamptz,external_org_id text);
CREATE TABLE research_erased_resources(digest text PRIMARY KEY);
INSERT INTO tasks VALUES ('1','active',now()-interval '719 hours',NULL,NULL,'tenant'),('2','due',now()-interval '721 hours',NULL,NULL,'tenant');
UPDATE tasks SET expires_at=created_at+interval '720 hours';
CREATE TABLE result_row_adjustments(task_uuid text,payload_json jsonb);
'''+deadline+';'+fence+';'+transform+''';
CREATE TRIGGER research_retention_deadline BEFORE INSERT OR UPDATE ON tasks FOR EACH ROW EXECUTE FUNCTION research_retention_deadline();
CREATE TRIGGER research_task_content_fence BEFORE INSERT OR UPDATE ON result_row_adjustments FOR EACH ROW EXECUTE FUNCTION research_task_content_fence('task_uuid');

UPDATE tasks SET created_at=now(),expires_at=now()+interval '1 year' WHERE uuid='due';
DO $$ DECLARE canonical jsonb; original jsonb; BEGIN
 IF EXISTS(SELECT 1 FROM tasks WHERE expires_at<>created_at+interval '720 hours') THEN RAISE EXCEPTION 'deadline extended'; END IF;
 IF EXISTS(SELECT 1 FROM tasks WHERE uuid='due' AND expires_at>now()) THEN RAISE EXCEPTION 'original creation replaced'; END IF;
 INSERT INTO result_row_adjustments VALUES ('active','{"focused":true}');
 BEGIN INSERT INTO result_row_adjustments VALUES ('due','{}'); RAISE EXCEPTION 'expired content accepted'; EXCEPTION WHEN check_violation THEN NULL; END;
 BEGIN INSERT INTO result_row_adjustments VALUES ('absent','{}'); RAISE EXCEPTION 'missing task recreated'; EXCEPTION WHEN check_violation THEN NULL; END;
 original='{"reported":true,"focused":false,"reportedBy":"actor","reportedAt":"2026-01-01","filters":[{"column":"reported","value":true}],"evidence":{"note":"reported by laboratory"}}';
 canonical=research_migrate_focus_json(original);
 IF canonical->'focused'<>'false'::jsonb OR canonical ? 'reported' OR canonical->>'focusedBy'<>'actor' OR canonical->>'focusedAt'<>'2026-01-01' OR canonical #>> '{filters,0,column}'<>'focused' OR canonical #>> '{evidence,note}'<>'reported by laboratory' THEN RAISE EXCEPTION 'focus state lost'; END IF;
 IF canonical<>research_migrate_focus_json(canonical) THEN RAISE EXCEPTION 'migration not idempotent'; END IF;
 INSERT INTO research_erased_resources VALUES(md5('tasks:tenant:active'));
 DELETE FROM tasks WHERE uuid='active';
 BEGIN INSERT INTO tasks VALUES ('1','active',now(),NULL,NULL,'tenant'); RAISE EXCEPTION 'erased task recreated'; EXCEPTION WHEN check_violation THEN NULL; END;
 BEGIN INSERT INTO tasks VALUES ('3','stale',now()-interval '721 hours',NULL,NULL,'tenant'); RAISE EXCEPTION 'expired task inserted'; EXCEPTION WHEN check_violation THEN NULL; END;
 BEGIN INSERT INTO result_row_adjustments VALUES ('active','{}'); RAISE EXCEPTION 'late content recreated'; EXCEPTION WHEN check_violation THEN NULL; END;
END $$;
SELECT 'RETENTION_SQL_BOUNDARIES_AND_MIGRATION_PASS';
ROLLBACK;'''
for function in ['research_retention_deadline','research_task_content_fence','research_migrate_focus_json']:
 sql=sql.replace(function+'(', 'pg_temp.'+function+'(')
# Additional isolated fixtures exercise the actual production guard functions.
append=(o/'internal/database/database.go').read_text(encoding='utf-8')
review_guard=re.search(r'DB.Exec\(`(CREATE OR REPLACE FUNCTION prevent_variant_review_event_mutation.*?)`\)',append,re.S).group(1)
squid=(o.parent/'Squid/internal/database/retention.go').read_text(encoding='utf-8')
admission_functions=re.findall(r'tx.Exec\(`(CREATE OR REPLACE FUNCTION.*?)`\)',squid,re.S)
extra="""
CREATE TABLE variant_review_events(id text,task_uuid text);
INSERT INTO variant_review_events VALUES ('v','due');
"""+review_guard+";"+"""
CREATE TRIGGER variant_review_events_append_only BEFORE UPDATE OR DELETE ON variant_review_events FOR EACH ROW EXECUTE FUNCTION prevent_variant_review_event_mutation();
DO $$ BEGIN
 BEGIN DELETE FROM variant_review_events; RAISE EXCEPTION 'unguarded deletion accepted'; EXCEPTION WHEN raise_exception THEN IF SQLERRM<>'variant review events are append-only' THEN RAISE; END IF; END;
 PERFORM set_config('research.expiry_task','due',true);
 DELETE FROM variant_review_events WHERE task_uuid='due';
 IF EXISTS(SELECT 1 FROM variant_review_events) THEN RAISE EXCEPTION 'due history not erased'; END IF;
END $$;
CREATE TABLE expired_task_guards(digest text PRIMARY KEY,admission_digest text,finalized bool);
CREATE TABLE cvm_execution_attempts(task_uuid text,org_id text);
CREATE TABLE overlay_task_states(uuid text,org_id text);
"""+';'.join(admission_functions)+";"+"""
CREATE TRIGGER research_expiry_guard_lock BEFORE INSERT OR UPDATE ON expired_task_guards FOR EACH ROW EXECUTE FUNCTION research_expiry_guard_lock();
CREATE TRIGGER research_execution_admission BEFORE INSERT ON cvm_execution_attempts FOR EACH ROW EXECUTE FUNCTION research_execution_admission('task_uuid','all');
CREATE TRIGGER research_execution_admission BEFORE INSERT ON overlay_task_states FOR EACH ROW EXECUTE FUNCTION research_execution_admission('uuid','finalized');
INSERT INTO expired_task_guards VALUES ('opaque',md5('tenant:erased'),false);
DO $$ BEGIN
 BEGIN INSERT INTO cvm_execution_attempts VALUES ('erased','tenant'); RAISE EXCEPTION 'late dispatch admitted'; EXCEPTION WHEN check_violation THEN NULL; END;
 INSERT INTO cvm_execution_attempts VALUES ('erased','other-tenant');
 INSERT INTO overlay_task_states VALUES ('erased','tenant');
 UPDATE expired_task_guards SET finalized=true;
 DELETE FROM overlay_task_states;
 BEGIN INSERT INTO overlay_task_states VALUES ('erased','tenant'); RAISE EXCEPTION 'late callback recreated state'; EXCEPTION WHEN check_violation THEN NULL; END;
 INSERT INTO overlay_task_states VALUES ('erased','other-tenant');
END $$;
CREATE TABLE retention_jobs(id text PRIMARY KEY,due_at timestamptz,lease_id text,lease_until timestamptz,next_retry_at timestamptz,attempts int);
INSERT INTO retention_jobs VALUES ('task:due',now()-interval '1 second','',NULL,NULL,0);
DO $$ DECLARE claimed int; BEGIN
 UPDATE retention_jobs SET lease_id='one',lease_until=now()+interval '5 minutes',attempts=attempts+1 WHERE id='task:due' AND due_at<=now() AND (lease_until IS NULL OR lease_until<=now()) AND (next_retry_at IS NULL OR next_retry_at<=now());GET DIAGNOSTICS claimed=ROW_COUNT;
 IF claimed<>1 THEN RAISE EXCEPTION 'first worker cannot claim'; END IF;
 UPDATE retention_jobs SET lease_id='two' WHERE id='task:due' AND (lease_until IS NULL OR lease_until<=now());GET DIAGNOSTICS claimed=ROW_COUNT;
 IF claimed<>0 THEN RAISE EXCEPTION 'live lease stolen'; END IF;
 UPDATE retention_jobs SET lease_until=now()-interval '1 second',next_retry_at=now()-interval '1 second';
 UPDATE retention_jobs SET lease_id='two',attempts=attempts+1 WHERE id='task:due' AND (lease_until IS NULL OR lease_until<=now()) AND (next_retry_at IS NULL OR next_retry_at<=now());GET DIAGNOSTICS claimed=ROW_COUNT;
 IF claimed<>1 THEN RAISE EXCEPTION 'restart recovery failed'; END IF;
END $$;
SELECT 'ADMISSION_AUDIT_AND_QUEUE_PASS';
"""
for function in ['prevent_variant_review_event_mutation','research_expiry_guard_lock','research_execution_admission']:
 extra=extra.replace(function+'(', 'pg_temp.'+function+'(')
sql=sql.replace('ROLLBACK;',extra+'ROLLBACK;')

print(sql)
