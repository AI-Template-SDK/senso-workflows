#!/usr/bin/env python3
"""
Batch Fix Script - Recreated from batch_fix.log analysis

This script processes unbatched question runs and organizes them into batches
grouped by organization and date. It matches the behavior seen in batch_fix.log.

Usage:
    python batch_fix_recreated.py [--dry-run] [--single-org ORG_ID]
    
Examples:
    python batch_fix_recreated.py --dry-run  # Preview changes
    python batch_fix_recreated.py            # Apply changes
    python batch_fix_recreated.py --single-org 00011ca1-3ec6-4a4a-b44b-70e9d3fc829e
"""

import psycopg2
import psycopg2.extras
import uuid
import logging
import argparse
import sys
import os
from datetime import datetime, date
from collections import defaultdict
from typing import Dict, List, Tuple, Optional
from dotenv import load_dotenv

# Configure logging to match the original script's format
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s',
    datefmt='%Y-%m-%d %H:%M:%S'
)
logger = logging.getLogger(__name__)

class BatchFixProcessor:
    def __init__(self, database_url: str, dry_run: bool = False):
        self.database_url = database_url
        self.dry_run = dry_run
        self.conn = None
        self.cursor = None
        
        # Statistics
        self.stats = {
            'orgs_processed': 0,
            'batches_created': 0,
            'question_runs_updated': 0,
            'total_questions_processed': 0,
            'errors': []
        }
    
    def connect_to_database(self):
        """Connect to PostgreSQL database"""
        try:
            logger.info(f"Connecting to database using DATABASE_URL")
            
            self.conn = psycopg2.connect(self.database_url)
            self.conn.autocommit = False  # We want to control transactions
            self.cursor = self.conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor)
            
            # Get PostgreSQL version info
            self.cursor.execute("SELECT version()")
            version_result = self.cursor.fetchone()
            version_info = version_result['version']
            logger.info(f"Connected to PostgreSQL: {version_info}")
            
        except Exception as e:
            logger.error(f"Failed to connect to database: {e}")
            logger.error(f"Error type: {type(e).__name__}")
            if hasattr(e, 'pgcode'):
                logger.error(f"PostgreSQL error code: {e.pgcode}")
            raise
    
    def get_orgs_with_unbatched_runs(self) -> List[str]:
        """Get list of organization IDs that have unbatched question runs"""
        query = """
        SELECT DISTINCT 
            gq.org_id::text as org_id
        FROM question_runs qr
        JOIN geo_questions gq ON qr.geo_question_id = gq.geo_question_id
        WHERE qr.batch_id IS NULL
        AND gq.org_id IS NOT NULL
        and gq.deleted_at is null
        and qr.deleted_at is null
        ORDER BY org_id
        """
        
        self.cursor.execute(query)
        results = self.cursor.fetchall()
        org_ids = [row['org_id'] for row in results if row['org_id']]
        
        logger.info(f"Found {len(org_ids)} orgs with unbatched runs")
        return org_ids
    
    def get_unbatched_runs_for_org(self, org_id: str) -> List[dict]:
        """Get all unbatched question runs for a specific organization"""
        query = """
        SELECT 
            qr.question_run_id,
            qr.geo_question_id,
            qr.created_at::date as run_date,
            qr.created_at,
            gq.org_id::text as org_id
        FROM question_runs qr
        JOIN geo_questions gq ON qr.geo_question_id = gq.geo_question_id
        WHERE qr.batch_id IS NULL
        AND gq.org_id::text = %s
        AND gq.deleted_at is null
        AND qr.deleted_at is null
        ORDER BY qr.created_at::date, qr.created_at
        """
        
        self.cursor.execute(query, (org_id,))
        return self.cursor.fetchall()
    
    def check_existing_batch_for_date(self, org_id: str, batch_date: date) -> Optional[str]:
        """Check if a batch already exists for this org and date"""
        query = """
        SELECT batch_id FROM question_run_batches 
        WHERE org_id = %s 
        AND started_at::date = %s and deleted_at is null
        LIMIT 1
        """
        
        self.cursor.execute(query, (org_id, batch_date))
        result = self.cursor.fetchone()
        return result['batch_id'] if result else None
    
    def group_runs_by_date(self, runs: List[dict]) -> Dict[date, List[dict]]:
        """Group question runs by their creation date"""
        grouped = defaultdict(list)
        for run in runs:
            grouped[run['run_date']].append(run)
        return dict(grouped)
    
    def create_batch(self, org_id: str, batch_date: date, question_count: int) -> str:
        """Create a new question run batch"""
        batch_id = str(uuid.uuid4())
        
        if self.dry_run:
            return batch_id
        
        # Insert batch record
        insert_query = """
        INSERT INTO question_run_batches (
            batch_id, scope, org_id, batch_type, status, 
            total_questions, completed_questions, failed_questions,
            is_latest, created_at, updated_at
        ) VALUES (
            %s, 'org', %s, 'migration', 'completed',
            %s, %s, 0,
            false, %s, %s
        )
        """
        
        now = datetime.now()
        self.cursor.execute(insert_query, (
            batch_id, org_id, question_count, question_count, now, now
        ))
        
        logger.info(f"Created batch {batch_id} for org {org_id} on {batch_date}")
        return batch_id
    
    def update_question_runs_with_batch(self, runs: List[dict], batch_id: str) -> int:
        """Update question runs to associate them with a batch"""
        if self.dry_run:
            return len(runs)
        
        run_ids = [str(run['question_run_id']) for run in runs]
        
        # Update question runs with batch_id (cast UUIDs properly)
        update_query = """
        UPDATE question_runs 
        SET batch_id = %s::uuid, updated_at = %s
        WHERE question_run_id = ANY(%s::uuid[])
        AND deleted_at is null
        """
        
        self.cursor.execute(update_query, (batch_id, datetime.now(), run_ids))
        updated_count = self.cursor.rowcount
        
        logger.info(f"Updated {updated_count} question runs with batch_id {batch_id}")
        return updated_count
    
    def update_latest_flags_for_org(self, org_id: str) -> int:
        """Update is_latest flags for batches of an organization"""
        if self.dry_run:
            return 1  # Simulate marking 1 batch as latest
        
        # First, set all batches for this org to not latest
        self.cursor.execute("""
            UPDATE question_run_batches 
            SET is_latest = false, updated_at = %s
            WHERE org_id = %s 
            AND deleted_at is null
        """, (datetime.now(), org_id))
        
        # Then set the most recent batch as latest
        self.cursor.execute("""
            UPDATE question_run_batches 
            SET is_latest = true, updated_at = %s
            WHERE batch_id = (
                SELECT batch_id 
                FROM question_run_batches 
                WHERE org_id = %s and deleted_at is null
                ORDER BY created_at DESC 
                LIMIT 1
            )
        """, (datetime.now(), org_id))
        
        latest_count = self.cursor.rowcount
        logger.info(f"Updated latest flags for org {org_id}: {latest_count} batch marked as latest")
        return latest_count
    
    def check_org_exists(self, org_id: str) -> bool:
        """Check if organization exists and is not deleted"""
        try:
            self.cursor.execute("""
                SELECT org_id FROM orgs 
                WHERE org_id = %s AND deleted_at IS NULL
            """, (org_id,))
            return self.cursor.fetchone() is not None
        except Exception:
            # If orgs table doesn't exist or has different structure, assume org exists
            return True
    
    def process_org(self, org_id: str) -> bool:
        """Process batches for a single organization"""
        try:
            # Check if org exists
            if not self.check_org_exists(org_id):
                error_msg = f"Org {org_id} does not exist or is deleted"
                logger.error(f"❌ Rolled back changes for org {org_id}")
                logger.error(f"Failed to process org {org_id}: {error_msg}")
                self.stats['errors'].append(f"Org {org_id}: {error_msg}")
                return False
            
            prefix = "[DRY RUN] " if self.dry_run else ""
            logger.info(f"{prefix}Processing batches for org: {org_id}")
            
            # Get unbatched runs for this org
            runs = self.get_unbatched_runs_for_org(org_id)
            if not runs:
                logger.info(f"{prefix}No unbatched runs found for org {org_id}")
                return True
            
            # Group runs by date
            grouped_runs = self.group_runs_by_date(runs)
            logger.info(f"Found {len(grouped_runs)} batch groups for org {org_id}")
            
            total_runs_updated = 0
            batches_created = 0
            
            # Process each date group
            for batch_date, date_runs in sorted(grouped_runs.items()):
                question_count = len(date_runs)
                logger.info(f"{prefix}Processing batch for {batch_date}: {question_count} questions, {question_count} runs")
                
                if not self.dry_run:
                    # Check if batch already exists for this date
                    existing_batch_id = self.check_existing_batch_for_date(org_id, batch_date)
                    
                    if existing_batch_id:
                        # Use existing batch
                        batch_id = existing_batch_id
                        logger.info(f"Using existing batch {batch_id} for org {org_id} on {batch_date}")
                    else:
                        # Create new batch
                        batch_id = self.create_batch(org_id, batch_date, question_count)
                        batches_created += 1
                    
                    # Update question runs with the batch (existing or new)
                    updated_count = self.update_question_runs_with_batch(date_runs, batch_id)
                    total_runs_updated += updated_count
                else:
                    # In dry run, assume we would create a new batch
                    total_runs_updated += question_count
                    batches_created += 1
            
            # Update latest flags
            if not self.dry_run and batches_created > 0:
                self.update_latest_flags_for_org(org_id)
            
            logger.info(f"{prefix}Completed processing for org {org_id}: {batches_created} batches, {total_runs_updated} runs updated")
            
            if not self.dry_run:
                logger.info(f"✅ Committed changes for org {org_id}")
            
            # Update statistics
            self.stats['orgs_processed'] += 1
            self.stats['batches_created'] += batches_created
            self.stats['question_runs_updated'] += total_runs_updated
            self.stats['total_questions_processed'] += total_runs_updated
            
            return True
            
        except Exception as e:
            if not self.dry_run:
                logger.error(f"❌ Rolled back changes for org {org_id}")
            error_msg = str(e)
            logger.error(f"Failed to process org {org_id}: {error_msg}")
            self.stats['errors'].append(f"Org {org_id}: {error_msg}")
            return False
    
    def print_final_summary(self):
        """Print final processing summary"""
        logger.info("")
        logger.info("=" * 60)
        logger.info("FINAL SUMMARY")
        logger.info("=" * 60)
        
        prefix = "[DRY RUN] " if self.dry_run else ""
        logger.info(f"{prefix}Orgs processed: {self.stats['orgs_processed']}")
        logger.info(f"{prefix}Batches created: {self.stats['batches_created']}")
        logger.info(f"{prefix}Question runs updated: {self.stats['question_runs_updated']}")
        logger.info(f"{prefix}Total questions processed: {self.stats['total_questions_processed']}")
        
        if self.stats['errors']:
            logger.error(f"Errors encountered: {len(self.stats['errors'])}")
            for error in self.stats['errors']:
                logger.error(f"  - {error}")
    
    def process_all_orgs(self):
        """Process all organizations with unbatched runs"""
        try:
            self.connect_to_database()
            
            # Get all orgs with unbatched runs
            org_ids = self.get_orgs_with_unbatched_runs()
            
            if not org_ids:
                logger.info("No organizations with unbatched runs found")
                return
            
            # Process each organization
            for org_id in org_ids:
                try:
                    if not self.dry_run:
                        # Start transaction for this org
                        self.conn.rollback()  # Ensure clean state
                    
                    success = self.process_org(org_id)
                    
                    if not self.dry_run:
                        if success:
                            self.conn.commit()
                        else:
                            self.conn.rollback()
                
                except Exception as e:
                    if not self.dry_run:
                        self.conn.rollback()
                    logger.error(f"Error processing org {org_id}: {e}")
                    self.stats['errors'].append(f"Org {org_id}: {str(e)}")
            
            self.print_final_summary()
            
        finally:
            if self.cursor:
                self.cursor.close()
            if self.conn:
                self.conn.close()
    
    def process_single_org(self, org_id: str):
        """Process a single organization"""
        try:
            self.connect_to_database()
            
            try:
                if not self.dry_run:
                    self.conn.rollback()  # Ensure clean state
                
                success = self.process_org(org_id)
                
                if not self.dry_run:
                    if success:
                        self.conn.commit()
                    else:
                        self.conn.rollback()
            
            except Exception as e:
                if not self.dry_run:
                    self.conn.rollback()
                logger.error(f"Error processing org {org_id}: {e}")
                self.stats['errors'].append(f"Org {org_id}: {str(e)}")
            
            self.print_final_summary()
            
        finally:
            if self.cursor:
                self.cursor.close()
            if self.conn:
                self.conn.close()


def main():
    # Load environment variables from .env file
    load_dotenv()
    
    parser = argparse.ArgumentParser(
        description="Batch Fix Script - Process unbatched question runs",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
    python batch_fix_recreated.py --dry-run
    python batch_fix_recreated.py
    python batch_fix_recreated.py --single-org 00011ca1-3ec6-4a4a-b44b-70e9d3fc829e
        """
    )
    
    parser.add_argument(
        '--dry-run',
        action='store_true',
        help='Preview changes without applying them'
    )
    
    parser.add_argument(
        '--single-org',
        help='Process only a specific organization ID'
    )
    
    args = parser.parse_args()
    
    # Get database URL from environment
    database_url = os.getenv('DATABASE_URL')
    if not database_url:
        logger.error("DATABASE_URL environment variable not found. Please set it in your .env file.")
        logger.error("Make sure you have a .env file with: DATABASE_URL=postgresql://user:password@host:port/database")
        sys.exit(1)
    
    logger.info(f"Using DATABASE_URL: {database_url[:20]}...{database_url[-10:]}")  # Log partial URL for debugging
    
    # Create processor
    processor = BatchFixProcessor(database_url, dry_run=args.dry_run)
    
    try:
        if args.single_org:
            processor.process_single_org(args.single_org)
        else:
            processor.process_all_orgs()
    
    except KeyboardInterrupt:
        logger.info("\nScript interrupted by user")
        sys.exit(1)
    except Exception as e:
        logger.error(f"Script failed: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main() 