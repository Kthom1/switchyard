"""Provision one fresh local Plane instance from private Switchyard metadata.

Run through `python manage.py shell -c` in the pinned Plane v1.4.2 API image.
This follows plane/license/api/views/admin.py and plane/app/views/project/base.py;
keep this small direct-model integration covered when updating the Plane image.
"""

import json
import re
import sys
from uuid import UUID

from django.contrib.auth.hashers import make_password
from django.core.validators import validate_email
from django.db import transaction
from django.utils import timezone

from plane.db.models import (
    APIToken, Label, Profile, Project, ProjectIdentifier, ProjectMember,
    State, User, Workspace, WorkspaceMember,
)
from plane.db.models.state import DEFAULT_STATES
from plane.license.models import Instance, InstanceAdmin, InstanceConfiguration
from plane.utils.cache import invalidate_cache_directly


def require(condition, message):
    if not condition:
        raise SystemExit(message)


def create_project(workspace, owner, automation, project_id, identifier, name):
    project = Project.objects.create(id=project_id, workspace=workspace, name=name,
                                     identifier=identifier, network=0)
    ProjectIdentifier.objects.create(project=project, workspace=workspace, name=identifier)
    for user, role in ((owner, 20), (automation, 15)):
        ProjectMember.objects.create(project=project, workspace=workspace, member=user, role=role)
    states = DEFAULT_STATES + [
        {"name": "Human Review", "color": "#8E4EC6", "sequence": 37500, "group": "started"},
        {"name": "Blocked", "color": "#E5484D", "sequence": 40000, "group": "started"},
    ]
    State.objects.bulk_create([State(project=project, workspace=workspace, **state) for state in states])
    project.default_state = State.objects.get(project=project, default=True)
    project.save(update_fields=["default_state"])
    Label.objects.create(project=project, workspace=workspace, name="agent", color="#3E63DD")
    return project


def bootstrap():
    raw = json.load(sys.stdin)
    request = None
    if isinstance(raw, dict) and "board" in raw:
        require(set(raw) == {"board", "project"}, "Invalid project setup request.")
        data, request = raw["board"], raw["project"]
        require(isinstance(request, dict) and set(request) == {"id", "identifier", "name", "existing"}
                and all(isinstance(request[key], str) for key in ("id", "identifier", "name"))
                and isinstance(request["existing"], bool), "Invalid project setup request.")
        UUID(request["id"])
        require(re.fullmatch(r"[A-Z][A-Z0-9_]{0,11}", request["identifier"])
                and 1 <= len(request["name"]) <= 255, "Invalid project name or identifier.")
    else:
        data = raw
    fields = {
        "workspace", "workspace_id", "project_id", "identifier", "owner_email",
        "owner_id", "owner_password", "automation_id", "api_key",
    }
    require(isinstance(data, dict) and set(data) == fields
            and all(isinstance(value, str) for value in data.values()),
            "Invalid local Plane setup metadata.")
    ids = [UUID(data[field]) for field in
           ("workspace_id", "project_id", "owner_id", "automation_id")]
    require(len(set(ids)) == len(ids), "Local Plane identities must be distinct.")
    require(re.fullmatch(r"[a-z0-9][a-z0-9_-]{0,47}", data["workspace"]),
            "Invalid local Plane workspace slug.")
    require(re.fullmatch(r"[A-Z][A-Z0-9_]{0,11}", data["identifier"]),
            "Invalid local Plane project identifier.")
    validate_email(data["owner_email"])
    require(data["owner_email"] == data["owner_email"].lower()
            and data["owner_email"] != "symphony@switchyard.local",
            "Invalid local Plane owner email.")
    require(16 <= len(data["owner_password"]) <= 1024,
            "Invalid local Plane owner password.")
    require(re.fullmatch(r"plane_api_(?:[0-9a-f]{32}|[0-9a-f]{64})", data["api_key"]),
            "Invalid local Plane automation key.")

    with transaction.atomic():
        # The API entrypoint registers this row before it starts serving requests.
        # Upstream initial signup locks the same row to serialize registration.
        instance = Instance.objects.select_for_update().first()
        require(instance is not None, "Plane instance registration is incomplete.")
        owner = User.objects.filter(pk=data["owner_id"]).first()
        if owner is not None:
            # A repeat verifies ownership without resetting passwords, memberships,
            # or a revoked token. Later user-created projects and accounts are fine.
            automation = User.objects.filter(pk=data["automation_id"],
                email="symphony@switchyard.local", is_active=True,
                is_superuser=False, is_staff=False).first()
            valid = (
                instance.is_setup_done
                and owner.email == data["owner_email"] and owner.is_active
                and not owner.is_bot
                and InstanceAdmin.objects.filter(instance=instance, user=owner, role=20).exists()
                and Workspace.objects.filter(pk=data["workspace_id"],
                                             slug=data["workspace"], owner=owner).exists()
                and automation is not None
                and not InstanceAdmin.objects.filter(user_id=data["automation_id"]).exists()
            )
            for user_id, role in ((data["owner_id"], 20), (data["automation_id"], 15)):
                valid = valid and WorkspaceMember.objects.filter(
                    workspace_id=data["workspace_id"], member_id=user_id,
                    role=role, is_active=True).exists()
                if request is None:
                    valid = valid and ProjectMember.objects.filter(
                        project_id=data["project_id"], workspace_id=data["workspace_id"],
                        member_id=user_id, role=role, is_active=True).exists()
            if request is None:
                valid = valid and Project.objects.filter(pk=data["project_id"],
                    workspace_id=data["workspace_id"], identifier=data["identifier"]).exists()
            token = APIToken.objects.filter(token=data["api_key"],
                                            user_id=data["automation_id"],
                                            workspace_id=data["workspace_id"],
                                            is_active=True).first()
            valid = valid and token is not None and (
                token.expired_at is None or token.expired_at > timezone.now())
            # Earlier installations used a regular service user with no usable
            # password and a personal token. Preserve that exact non-admin
            # identity on restore; fresh setup still creates the native bot form.
            native = automation is not None and token is not None and (
                automation.is_bot and token.user_type == 1 and len(data["api_key"]) == 74)
            legacy = automation is not None and token is not None and (
                not automation.is_bot and not automation.has_usable_password()
                and token.user_type == 0 and len(data["api_key"]) == 42)
            valid = valid and (native or legacy)
            require(valid, "Local Plane setup no longer matches this installation; no changes made.")
        else:
            require(re.fullmatch(r"plane_api_[0-9a-f]{64}", data["api_key"]),
                    "Fresh local Plane setup requires a new automation key.")
            require(not instance.is_setup_done and not InstanceAdmin.objects.exists()
                    and not User.objects.exists() and not Workspace.objects.exists()
                    and not Project.objects.exists(),
                    "Plane already contains another setup; automatic setup requires a fresh instance.")
            owner = User.objects.create(
                id=data["owner_id"], username=UUID(data["owner_id"]).hex,
                email=data["owner_email"], password=make_password(data["owner_password"]),
                first_name="Switchyard", last_name="Owner", display_name="Switchyard Owner",
                is_password_autoset=False, is_email_verified=True,
            )
            InstanceAdmin.objects.create(instance=instance, user=owner, role=20)
            workspace = Workspace.objects.create(
                id=data["workspace_id"], name="Switchyard", slug=data["workspace"], owner=owner,
            )
            automation = User.objects.create(
                id=data["automation_id"], username=UUID(data["automation_id"]).hex,
                email="symphony@switchyard.local", password=make_password(None),
                display_name="Symphony", is_bot=True,
            )
            for user, role in ((owner, 20), (automation, 15)):
                WorkspaceMember.objects.create(workspace=workspace, member=user, role=role)
            create_project(workspace, owner, automation, data["project_id"], data["identifier"], "Tasks")
            Profile.objects.create(
                user=owner, is_onboarded=True, is_tour_completed=True,
                last_workspace_id=workspace.id,
                onboarding_step={"profile_complete": True, "workspace_create": True,
                                 "workspace_invite": True, "workspace_join": True},
            )
            APIToken.objects.create(
                user=automation, workspace=workspace, token=data["api_key"], user_type=1,
                label="Switchyard", allowed_rate_limit="600/minute",
            )
            for key, value in (("ENABLE_SIGNUP", "0"), ("ENABLE_EMAIL_PASSWORD", "1"),
                               ("ENABLE_MAGIC_LINK_LOGIN", "0")):
                InstanceConfiguration.objects.update_or_create(
                    key=key, defaults={"value": value, "category": "AUTHENTICATION",
                                       "is_encrypted": False},
                )
            instance.is_setup_done = True
            instance.save(update_fields=["is_setup_done"])

        if request is not None:
            workspace = Workspace.objects.get(pk=data["workspace_id"])
            automation = User.objects.get(pk=data["automation_id"])
            project = Project.objects.filter(pk=request["id"]).first()
            if project is None:
                require(not request["existing"], "The requested project does not exist in this workspace.")
                require(not ProjectIdentifier.objects.filter(workspace=workspace, name=request["identifier"]).exists(),
                        "Project identifier already exists; choose another --identifier.")
                create_project(workspace, owner, automation, request["id"], request["identifier"], request["name"])
            else:
                require(project.workspace_id == workspace.id and project.identifier == request["identifier"],
                        "Project identity does not match this workspace and identifier; no changes made.")
                if request["existing"]:
                    require(ProjectMember.objects.filter(project=project, member=owner, role=20, is_active=True).exists(),
                            "The local board owner must administer the requested project.")
                    membership, created = ProjectMember.objects.get_or_create(project=project, workspace=workspace,
                        member=automation, defaults={"role": 15})
                    require(created or (membership.role == 15 and membership.is_active),
                            "Existing automation project membership differs; no changes made.")
                    # Existing projects retain their states; add only missing workflow conventions.
                    for name, color, sequence in (("Human Review", "#8E4EC6", 37500), ("Blocked", "#E5484D", 40000)):
                        State.objects.get_or_create(project=project, workspace=workspace, name=name,
                            defaults={"color": color, "sequence": sequence, "group": "started"})
                    Label.objects.get_or_create(project=project, workspace=workspace, name="agent", defaults={"color": "#3E63DD"})
                else:
                    for user, role in ((owner, 20), (automation, 15)):
                        require(ProjectMember.objects.filter(project=project, workspace=workspace,
                            member=user, role=role, is_active=True).exists(),
                            "Existing project membership differs; no changes made.")

    # Readiness may have cached the pre-registration response for two hours.
    invalidate_cache_directly(path="/api/instances/", user=False)


try:
    bootstrap()
except Exception:
    # Never let a database error echo the JSON credentials or SQL parameters.
    raise SystemExit("Local Plane setup failed; retained private metadata can be used to retry.") from None
