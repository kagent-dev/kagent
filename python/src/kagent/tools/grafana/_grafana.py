from enum import Enum
from typing import Any, Dict, List, Optional, Type, TypeVar

import httpx
from autogen_core import CancellationToken, Component
from autogen_core.tools import BaseTool as BaseCoreTool
from pydantic import BaseModel, Field


class Config(BaseModel):
    """Base configuration for all Grafana tools"""

    base_url: str = Field(default="http://localhost:3000/api", description="The base URL of the Grafana API")
    username: str = Field(default="", description="Username for basic auth")
    password: str = Field(default="", description="Password for basic auth")
    api_key: str = Field(default="", description="API key for token auth")


def get_http_client(config: Config, cancellation_token: CancellationToken) -> httpx.AsyncClient:
    """Create an HTTP client for the API with appropriate authentication"""
    headers = {}

    # Set auth based on provided credentials
    auth = None
    if config.api_key:
        # Bearer token authentication
        headers["Authorization"] = f"Bearer {config.api_key}"
    elif config.username and config.password:
        # Basic authentication
        auth = httpx.BasicAuth(config.username, config.password)

    return httpx.AsyncClient(base_url=config.base_url, auth=auth, headers=headers)


ArgsT = TypeVar("ArgsT", bound=BaseModel, contravariant=True)


class BaseTool(BaseCoreTool[ArgsT, BaseModel], Component[Config]):
    """Base class for all Grafana tools"""

    component_type = "tool"
    component_config_schema = Config

    @property
    def component_provider_override(self) -> str:
        """Build the component provider path from the class name"""
        return f"kagent.tools.grafana.{self.__class__.__name__}"

    def __init__(
        self,
        config: Config,
        input_model: Type[ArgsT],
        description: str,
    ) -> None:
        super().__init__(input_model, BaseModel, self.__class__.__name__, description)
        self.config = config

    def _to_config(self) -> Config:
        """Convert to config object"""
        return self.config.model_copy()

    @classmethod
    def _from_config(cls, config: Config) -> "BaseTool":
        """Create instance from config"""
        raise NotImplementedError("Use specific tool implementations")


#######################
# Organization Management #
#######################


class OrgAction(str, Enum):
    """Possible organization actions"""

    GET_CURRENT = "get_current"
    UPDATE_CURRENT = "update_current"
    GET_USERS = "get_users"
    ADD_USER = "add_user"
    UPDATE_USER = "update_user"
    DELETE_USER = "delete_user"
    GET_PREFERENCES = "get_preferences"
    UPDATE_PREFERENCES = "update_preferences"
    LIST_ORGS = "list_orgs"
    GET_ORG = "get_org"
    CREATE_ORG = "create_org"
    UPDATE_ORG = "update_org"
    DELETE_ORG = "delete_org"


class OrgManagementInput(BaseModel):
    """Input for organization management operations"""

    action: OrgAction = Field(description="The organization action to perform")

    # Org ID for specific org operations
    org_id: Optional[int] = Field(default=None, description="Organization ID")

    # Org update parameters
    name: Optional[str] = Field(default=None, description="Organization name")

    # User operations in org
    user_id: Optional[int] = Field(default=None, description="User ID for operations on specific users")
    login_or_email: Optional[str] = Field(default=None, description="User login or email for adding to org")
    role: Optional[str] = Field(default=None, description="Role in the organization (Admin, Editor, Viewer)")

    # Preferences parameters
    theme: Optional[str] = Field(default=None, description="Theme preference")
    home_dashboard_id: Optional[int] = Field(default=None, description="Home dashboard ID")
    home_dashboard_uid: Optional[str] = Field(default=None, description="Home dashboard UID")
    timezone: Optional[str] = Field(default=None, description="Timezone preference")

    # List orgs parameters
    query: Optional[str] = Field(default=None, description="Search query for organizations")
    page: Optional[int] = Field(default=None, description="Page number")
    perpage: Optional[int] = Field(default=None, description="Number of items per page")


class OrgManagementTool(BaseTool):
    """Tool for managing Grafana organizations"""

    _description = """Perform various operations related to Grafana organizations including:
    - get_current: Get current organization information
    - update_current: Update current organization
    - get_users: Get users in the current organization
    - add_user: Add a user to the current organization
    - update_user: Update a user's role in the current organization
    - delete_user: Remove a user from the current organization
    - get_preferences: Get organization preferences
    - update_preferences: Update organization preferences
    - list_orgs: List all organizations (requires admin)
    - get_org: Get a specific organization by ID (requires admin)
    - create_org: Create a new organization (requires admin)
    - update_org: Update an organization (requires admin)
    - delete_org: Delete an organization (requires admin)
    """

    def __init__(self, config: Config) -> None:
        super().__init__(config=config, input_model=OrgManagementInput, description=self._description)

    async def run(self, args: OrgManagementInput, cancellation_token: CancellationToken) -> Any:
        async with get_http_client(self.config, cancellation_token) as client:
            # Handle different org operations based on the action
            if args.action == OrgAction.GET_CURRENT:
                return await self._get_current_org(client, args)
            elif args.action == OrgAction.UPDATE_CURRENT:
                return await self._update_current_org(client, args)
            elif args.action == OrgAction.GET_USERS:
                return await self._get_org_users(client, args)
            elif args.action == OrgAction.ADD_USER:
                return await self._add_org_user(client, args)
            elif args.action == OrgAction.UPDATE_USER:
                return await self._update_org_user(client, args)
            elif args.action == OrgAction.DELETE_USER:
                return await self._delete_org_user(client, args)
            elif args.action == OrgAction.GET_PREFERENCES:
                return await self._get_org_preferences(client, args)
            elif args.action == OrgAction.UPDATE_PREFERENCES:
                return await self._update_org_preferences(client, args)
            elif args.action == OrgAction.LIST_ORGS:
                return await self._list_orgs(client, args)
            elif args.action == OrgAction.GET_ORG:
                return await self._get_org(client, args)
            elif args.action == OrgAction.CREATE_ORG:
                return await self._create_org(client, args)
            elif args.action == OrgAction.UPDATE_ORG:
                return await self._update_org(client, args)
            elif args.action == OrgAction.DELETE_ORG:
                return await self._delete_org(client, args)
            else:
                raise ValueError(f"Unsupported organization action: {args.action}")

    async def _get_current_org(self, client: httpx.AsyncClient, args: OrgManagementInput) -> Any:
        response = await client.get("/org")
        response.raise_for_status()
        return response.json()

    async def _update_current_org(self, client: httpx.AsyncClient, args: OrgManagementInput) -> Any:
        if not args.name:
            raise ValueError("Organization name is required for update_current action")

        response = await client.put("/org", json={"name": args.name})
        response.raise_for_status()
        return response.json()

    async def _get_org_users(self, client: httpx.AsyncClient, args: OrgManagementInput) -> Any:
        response = await client.get("/org/users")
        response.raise_for_status()
        return response.json()

    async def _add_org_user(self, client: httpx.AsyncClient, args: OrgManagementInput) -> Any:
        if not args.login_or_email or not args.role:
            raise ValueError("User login/email and role are required for add_user action")

        payload = {"loginOrEmail": args.login_or_email, "role": args.role}

        response = await client.post("/org/users", json=payload)
        response.raise_for_status()
        return response.json()

    async def _update_org_user(self, client: httpx.AsyncClient, args: OrgManagementInput) -> Any:
        if not args.user_id or not args.role:
            raise ValueError("User ID and role are required for update_user action")

        payload = {"role": args.role}

        response = await client.patch(f"/org/users/{args.user_id}", json=payload)
        response.raise_for_status()
        return response.json()

    async def _delete_org_user(self, client: httpx.AsyncClient, args: OrgManagementInput) -> Any:
        if not args.user_id:
            raise ValueError("User ID is required for delete_user action")

        response = await client.delete(f"/org/users/{args.user_id}")
        response.raise_for_status()
        return response.json()

    async def _get_org_preferences(self, client: httpx.AsyncClient, args: OrgManagementInput) -> Any:
        response = await client.get("/org/preferences")
        response.raise_for_status()
        return response.json()

    async def _update_org_preferences(self, client: httpx.AsyncClient, args: OrgManagementInput) -> Any:
        payload = {}

        if args.theme:
            payload["theme"] = args.theme
        if args.home_dashboard_id:
            payload["homeDashboardId"] = args.home_dashboard_id
        if args.home_dashboard_uid:
            payload["homeDashboardUID"] = args.home_dashboard_uid
        if args.timezone:
            payload["timezone"] = args.timezone

        response = await client.put("/org/preferences", json=payload)
        response.raise_for_status()
        return response.json()

    async def _list_orgs(self, client: httpx.AsyncClient, args: OrgManagementInput) -> Any:
        params = {}

        if args.query:
            params["query"] = args.query
        if args.page:
            params["page"] = args.page
        if args.perpage:
            params["perpage"] = args.perpage

        response = await client.get("/orgs", params=params)
        response.raise_for_status()
        return response.json()

    async def _get_org(self, client: httpx.AsyncClient, args: OrgManagementInput) -> Any:
        if not args.org_id:
            raise ValueError("Organization ID is required for get_org action")

        response = await client.get(f"/orgs/{args.org_id}")
        response.raise_for_status()
        return response.json()

    async def _create_org(self, client: httpx.AsyncClient, args: OrgManagementInput) -> Any:
        if not args.name:
            raise ValueError("Organization name is required for create_org action")

        response = await client.post("/orgs", json={"name": args.name})
        response.raise_for_status()
        return response.json()

    async def _update_org(self, client: httpx.AsyncClient, args: OrgManagementInput) -> Any:
        if not args.org_id or not args.name:
            raise ValueError("Organization ID and name are required for update_org action")

        response = await client.put(f"/orgs/{args.org_id}", json={"name": args.name})
        response.raise_for_status()
        return response.json()

    async def _delete_org(self, client: httpx.AsyncClient, args: OrgManagementInput) -> Any:
        if not args.org_id:
            raise ValueError("Organization ID is required for delete_org action")

        response = await client.delete(f"/orgs/{args.org_id}")
        response.raise_for_status()
        return response.json()

    @classmethod
    def _from_config(cls, config: Config) -> "OrgManagementTool":
        return cls(config)


#######################
# Team Management #
#######################


class TeamAction(str, Enum):
    """Possible team actions"""

    SEARCH = "search"
    GET = "get"
    CREATE = "create"
    UPDATE = "update"
    DELETE = "delete"
    GET_MEMBERS = "get_members"
    ADD_MEMBER = "add_member"
    REMOVE_MEMBER = "remove_member"


class TeamManagementInput(BaseModel):
    """Input for team management operations"""

    action: TeamAction = Field(description="The team action to perform")

    # Team ID for specific team operations
    team_id: Optional[str] = Field(default=None, description="Team ID")

    # Search parameters
    query: Optional[str] = Field(default=None, description="Search query for teams")
    name: Optional[str] = Field(default=None, description="Team name filter")
    page: Optional[int] = Field(default=None, description="Page number")
    perpage: Optional[int] = Field(default=None, description="Number of items per page")

    # Create/update parameters
    team_name: Optional[str] = Field(default=None, description="Team name")
    email: Optional[str] = Field(default=None, description="Team email")
    org_id: Optional[int] = Field(default=None, description="Organization ID")

    # Member operations
    user_id: Optional[int] = Field(default=None, description="User ID for member operations")


class TeamManagementTool(BaseTool):
    """Tool for managing Grafana teams"""

    _description = """Perform various operations related to Grafana teams including:
    - search: Search for teams with filtering and pagination
    - get: Get a specific team by ID
    - create: Create a new team
    - update: Update an existing team
    - delete: Delete a team
    - get_members: Get members of a team
    - add_member: Add a user to a team
    - remove_member: Remove a user from a team
    """

    def __init__(self, config: Config) -> None:
        super().__init__(config=config, input_model=TeamManagementInput, description=self._description)

    async def run(self, args: TeamManagementInput, cancellation_token: CancellationToken) -> Any:
        async with get_http_client(self.config, cancellation_token) as client:
            # Handle different team operations based on the action
            if args.action == TeamAction.SEARCH:
                return await self._search_teams(client, args)
            elif args.action == TeamAction.GET:
                return await self._get_team(client, args)
            elif args.action == TeamAction.CREATE:
                return await self._create_team(client, args)
            elif args.action == TeamAction.UPDATE:
                return await self._update_team(client, args)
            elif args.action == TeamAction.DELETE:
                return await self._delete_team(client, args)
            elif args.action == TeamAction.GET_MEMBERS:
                return await self._get_team_members(client, args)
            elif args.action == TeamAction.ADD_MEMBER:
                return await self._add_team_member(client, args)
            elif args.action == TeamAction.REMOVE_MEMBER:
                return await self._remove_team_member(client, args)
            else:
                raise ValueError(f"Unsupported team action: {args.action}")

    async def _search_teams(self, client: httpx.AsyncClient, args: TeamManagementInput) -> Any:
        params = {}

        if args.query:
            params["query"] = args.query
        if args.name:
            params["name"] = args.name
        if args.page:
            params["page"] = args.page
        if args.perpage:
            params["perpage"] = args.perpage

        response = await client.get("/teams/search", params=params)
        response.raise_for_status()
        return response.json()

    async def _get_team(self, client: httpx.AsyncClient, args: TeamManagementInput) -> Any:
        if not args.team_id:
            raise ValueError("Team ID is required for get action")

        response = await client.get(f"/teams/{args.team_id}")
        response.raise_for_status()
        return response.json()

    async def _create_team(self, client: httpx.AsyncClient, args: TeamManagementInput) -> Any:
        if not args.team_name:
            raise ValueError("Team name is required for create action")

        payload = {"name": args.team_name}

        if args.email:
            payload["email"] = args.email
        if args.org_id:
            payload["orgId"] = args.org_id

        response = await client.post("/teams", json=payload)
        response.raise_for_status()
        return response.json()

    async def _update_team(self, client: httpx.AsyncClient, args: TeamManagementInput) -> Any:
        if not args.team_id or not args.team_name:
            raise ValueError("Team ID and name are required for update action")

        payload = {"name": args.team_name}

        if args.email:
            payload["email"] = args.email

        response = await client.put(f"/teams/{args.team_id}", json=payload)
        response.raise_for_status()
        return response.json()

    async def _delete_team(self, client: httpx.AsyncClient, args: TeamManagementInput) -> Any:
        if not args.team_id:
            raise ValueError("Team ID is required for delete action")

        response = await client.delete(f"/teams/{args.team_id}")
        response.raise_for_status()
        return response.json()

    async def _get_team_members(self, client: httpx.AsyncClient, args: TeamManagementInput) -> Any:
        if not args.team_id:
            raise ValueError("Team ID is required for get_members action")

        response = await client.get(f"/teams/{args.team_id}/members")
        response.raise_for_status()
        return response.json()

    async def _add_team_member(self, client: httpx.AsyncClient, args: TeamManagementInput) -> Any:
        if not args.team_id or not args.user_id:
            raise ValueError("Team ID and user ID are required for add_member action")

        payload = {"userId": args.user_id}

        response = await client.post(f"/teams/{args.team_id}/members", json=payload)
        response.raise_for_status()
        return response.json()

    async def _remove_team_member(self, client: httpx.AsyncClient, args: TeamManagementInput) -> Any:
        if not args.team_id or not args.user_id:
            raise ValueError("Team ID and user ID are required for remove_member action")

        response = await client.delete(f"/teams/{args.team_id}/members/{args.user_id}")
        response.raise_for_status()
        return response.json()

    @classmethod
    def _from_config(cls, config: Config) -> "TeamManagementTool":
        return cls(config)


#########################
# Alert Management #
#########################


class AlertAction(str, Enum):
    """Possible alert actions"""

    GET_RULES = "get_rules"
    GET_RULE = "get_rule"
    CREATE_RULE = "create_rule"
    UPDATE_RULE = "update_rule"
    DELETE_RULE = "delete_rule"
    GET_RULE_GROUP = "get_rule_group"
    GET_CONTACT_POINTS = "get_contact_points"
    CREATE_CONTACT_POINT = "create_contact_point"
    UPDATE_CONTACT_POINT = "update_contact_point"
    DELETE_CONTACT_POINT = "delete_contact_point"
    GET_NOTIFICATION_POLICIES = "get_notification_policies"
    UPDATE_NOTIFICATION_POLICIES = "update_notification_policies"
    GET_MUTE_TIMINGS = "get_mute_timings"
    GET_MUTE_TIMING = "get_mute_timing"
    CREATE_MUTE_TIMING = "create_mute_timing"
    DELETE_MUTE_TIMING = "delete_mute_timing"


class AlertManagementInput(BaseModel):
    """Input for alert management operations"""

    action: AlertAction = Field(description="The alert action to perform")

    # Rule ID for specific rule operations
    rule_uid: Optional[str] = Field(default=None, description="Alert rule UID")

    # Rule group parameters
    folder_uid: Optional[str] = Field(default=None, description="Folder UID for rule group")
    group: Optional[str] = Field(default=None, description="Rule group name")

    # Create/update rule parameters
    title: Optional[str] = Field(default=None, description="Title of the alert rule")
    condition: Optional[str] = Field(default=None, description="Condition expression for the alert rule")
    data: Optional[List[Dict[str, Any]]] = Field(default=None, description="Data queries for the alert rule")
    no_data_state: Optional[str] = Field(default=None, description="State when no data is returned")
    exec_err_state: Optional[str] = Field(default=None, description="State when execution error occurs")
    for_duration: Optional[str] = Field(default=None, description="Duration condition must be met before alerting")
    annotations: Optional[Dict[str, str]] = Field(default=None, description="Annotations for the alert")
    labels: Optional[Dict[str, str]] = Field(default=None, description="Labels for the alert")

    # Contact point parameters
    contact_point_uid: Optional[str] = Field(default=None, description="Contact point UID")
    contact_point_name: Optional[str] = Field(default=None, description="Name of the contact point")
    contact_point_type: Optional[str] = Field(
        default=None, description="Type of contact point (email, slack, webhook, etc.)"
    )
    contact_point_settings: Optional[Dict[str, Any]] = Field(default=None, description="Settings for the contact point")

    # Notification policy parameters
    policy_tree: Optional[Dict[str, Any]] = Field(default=None, description="The complete notification policy tree")

    # Mute timing parameters
    mute_timing_name: Optional[str] = Field(default=None, description="Name of the mute timing")
    time_intervals: Optional[List[Dict[str, Any]]] = Field(default=None, description="Time intervals for mute timing")


class AlertManagementTool(BaseTool):
    """Tool for managing Grafana alerts"""

    _description = """Perform various operations related to Grafana alerting including:
    - get_rules: Get all alert rules
    - get_rule: Get a specific alert rule by UID
    - create_rule: Create a new alert rule
    - update_rule: Update an existing alert rule
    - delete_rule: Delete an alert rule
    - get_rule_group: Get an alert rule group
    - get_contact_points: Get all contact points
    - create_contact_point: Create a new contact point
    - update_contact_point: Update an existing contact point
    - delete_contact_point: Delete a contact point
    - get_notification_policies: Get the notification policy tree
    - update_notification_policies: Update the notification policy tree
    - get_mute_timings: Get all mute timings
    - get_mute_timing: Get a specific mute timing
    - create_mute_timing: Create a new mute timing
    - delete_mute_timing: Delete a mute timing
    """

    def __init__(self, config: Config) -> None:
        super().__init__(config=config, input_model=AlertManagementInput, description=self._description)

    async def run(self, args: AlertManagementInput, cancellation_token: CancellationToken) -> Any:
        async with get_http_client(self.config, cancellation_token) as client:
            # Handle different alert operations based on the action
            if args.action == AlertAction.GET_RULES:
                return await self._get_alert_rules(client, args)
            elif args.action == AlertAction.GET_RULE:
                return await self._get_alert_rule(client, args)
            elif args.action == AlertAction.CREATE_RULE:
                return await self._create_alert_rule(client, args)
            elif args.action == AlertAction.UPDATE_RULE:
                return await self._update_alert_rule(client, args)
            elif args.action == AlertAction.DELETE_RULE:
                return await self._delete_alert_rule(client, args)
            elif args.action == AlertAction.GET_RULE_GROUP:
                return await self._get_alert_rule_group(client, args)
            elif args.action == AlertAction.GET_CONTACT_POINTS:
                return await self._get_contact_points(client, args)
            elif args.action == AlertAction.CREATE_CONTACT_POINT:
                return await self._create_contact_point(client, args)
            elif args.action == AlertAction.UPDATE_CONTACT_POINT:
                return await self._update_contact_point(client, args)
            elif args.action == AlertAction.DELETE_CONTACT_POINT:
                return await self._delete_contact_point(client, args)
            elif args.action == AlertAction.GET_NOTIFICATION_POLICIES:
                return await self._get_notification_policies(client, args)
            elif args.action == AlertAction.UPDATE_NOTIFICATION_POLICIES:
                return await self._update_notification_policies(client, args)
            elif args.action == AlertAction.GET_MUTE_TIMINGS:
                return await self._get_mute_timings(client, args)
            elif args.action == AlertAction.GET_MUTE_TIMING:
                return await self._get_mute_timing(client, args)
            elif args.action == AlertAction.CREATE_MUTE_TIMING:
                return await self._create_mute_timing(client, args)
            elif args.action == AlertAction.DELETE_MUTE_TIMING:
                return await self._delete_mute_timing(client, args)
            else:
                raise ValueError(f"Unsupported alert action: {args.action}")

    async def _get_alert_rules(self, client: httpx.AsyncClient, args: AlertManagementInput) -> Any:
        response = await client.get("/v1/provisioning/alert-rules")
        response.raise_for_status()
        return response.json()

    async def _get_alert_rule(self, client: httpx.AsyncClient, args: AlertManagementInput) -> Any:
        if not args.rule_uid:
            raise ValueError("Alert rule UID is required for get_rule action")

        response = await client.get(f"/v1/provisioning/alert-rules/{args.rule_uid}")
        response.raise_for_status()
        return response.json()

    async def _create_alert_rule(self, client: httpx.AsyncClient, args: AlertManagementInput) -> Any:
        if not args.title or not args.folder_uid or not args.condition or not args.data:
            raise ValueError("Title, folder_uid, condition, and data are required for create_rule action")

        payload = {
            "title": args.title,
            "folderUID": args.folder_uid,
            "condition": args.condition,
            "data": args.data,
            "noDataState": args.no_data_state or "NoData",
            "execErrState": args.exec_err_state or "Error",
            "for": args.for_duration or "5m",
        }

        if args.annotations:
            payload["annotations"] = args.annotations
        if args.labels:
            payload["labels"] = args.labels

        response = await client.post("/v1/provisioning/alert-rules", json=payload)
        response.raise_for_status()
        return response.json()

    async def _update_alert_rule(self, client: httpx.AsyncClient, args: AlertManagementInput) -> Any:
        if not args.rule_uid:
            raise ValueError("Alert rule UID is required for update_rule action")

        # First get the current rule to avoid replacing all fields
        current_rule_response = await client.get(f"/v1/provisioning/alert-rules/{args.rule_uid}")
        current_rule_response.raise_for_status()
        current_rule = current_rule_response.json()

        # Update only the fields specified in the arguments
        payload = current_rule

        if args.title:
            payload["title"] = args.title
        if args.folder_uid:
            payload["folderUID"] = args.folder_uid
        if args.condition:
            payload["condition"] = args.condition
        if args.data:
            payload["data"] = args.data
        if args.no_data_state:
            payload["noDataState"] = args.no_data_state
        if args.exec_err_state:
            payload["execErrState"] = args.exec_err_state
        if args.for_duration:
            payload["for"] = args.for_duration
        if args.annotations:
            payload["annotations"] = args.annotations
        if args.labels:
            payload["labels"] = args.labels

        response = await client.put(f"/v1/provisioning/alert-rules/{args.rule_uid}", json=payload)
        response.raise_for_status()
        return response.json()

    async def _delete_alert_rule(self, client: httpx.AsyncClient, args: AlertManagementInput) -> Any:
        if not args.rule_uid:
            raise ValueError("Alert rule UID is required for delete_rule action")

        response = await client.delete(f"/v1/provisioning/alert-rules/{args.rule_uid}")
        response.raise_for_status()
        return {"status": "success", "message": "Alert rule deleted"}

    async def _get_alert_rule_group(self, client: httpx.AsyncClient, args: AlertManagementInput) -> Any:
        if not args.folder_uid or not args.group:
            raise ValueError("Folder UID and group name are required for get_rule_group action")

        response = await client.get(f"/v1/provisioning/folder/{args.folder_uid}/rule-groups/{args.group}")
        response.raise_for_status()
        return response.json()

    async def _get_contact_points(self, client: httpx.AsyncClient, args: AlertManagementInput) -> Any:
        params = {}
        if args.contact_point_name:
            params["name"] = args.contact_point_name

        response = await client.get("/v1/provisioning/contact-points", params=params)
        response.raise_for_status()
        return response.json()

    async def _create_contact_point(self, client: httpx.AsyncClient, args: AlertManagementInput) -> Any:
        if not args.contact_point_name or not args.contact_point_type or not args.contact_point_settings:
            raise ValueError("Contact point name, type, and settings are required for create_contact_point action")

        payload = {
            "name": args.contact_point_name,
            "type": args.contact_point_type,
            "settings": args.contact_point_settings,
        }

        response = await client.post("/v1/provisioning/contact-points", json=payload)
        response.raise_for_status()
        return response.json()

    async def _update_contact_point(self, client: httpx.AsyncClient, args: AlertManagementInput) -> Any:
        if (
            not args.contact_point_uid
            or not args.contact_point_name
            or not args.contact_point_type
            or not args.contact_point_settings
        ):
            raise ValueError("Contact point UID, name, type, and settings are required for update_contact_point action")

        payload = {
            "name": args.contact_point_name,
            "type": args.contact_point_type,
            "settings": args.contact_point_settings,
        }

        response = await client.put(f"/v1/provisioning/contact-points/{args.contact_point_uid}", json=payload)
        response.raise_for_status()
        return response.json()

    async def _delete_contact_point(self, client: httpx.AsyncClient, args: AlertManagementInput) -> Any:
        if not args.contact_point_uid:
            raise ValueError("Contact point UID is required for delete_contact_point action")

        response = await client.delete(f"/v1/provisioning/contact-points/{args.contact_point_uid}")
        response.raise_for_status()
        return {"status": "success", "message": "Contact point deleted"}

    async def _get_notification_policies(self, client: httpx.AsyncClient, args: AlertManagementInput) -> Any:
        response = await client.get("/v1/provisioning/policies")
        response.raise_for_status()
        return response.json()

    async def _update_notification_policies(self, client: httpx.AsyncClient, args: AlertManagementInput) -> Any:
        if not args.policy_tree:
            raise ValueError("Policy tree is required for update_notification_policies action")

        response = await client.put("/v1/provisioning/policies", json=args.policy_tree)
        response.raise_for_status()
        return response.json()

    async def _get_mute_timings(self, client: httpx.AsyncClient, args: AlertManagementInput) -> Any:
        response = await client.get("/v1/provisioning/mute-timings")
        response.raise_for_status()
        return response.json()

    async def _get_mute_timing(self, client: httpx.AsyncClient, args: AlertManagementInput) -> Any:
        if not args.mute_timing_name:
            raise ValueError("Mute timing name is required for get_mute_timing action")

        response = await client.get(f"/v1/provisioning/mute-timings/{args.mute_timing_name}")
        response.raise_for_status()
        return response.json()

    async def _create_mute_timing(self, client: httpx.AsyncClient, args: AlertManagementInput) -> Any:
        if not args.mute_timing_name or not args.time_intervals:
            raise ValueError("Mute timing name and time intervals are required for create_mute_timing action")

        payload = {"name": args.mute_timing_name, "time_intervals": args.time_intervals}

        response = await client.post("/v1/provisioning/mute-timings", json=payload)
        response.raise_for_status()
        return response.json()

    async def _delete_mute_timing(self, client: httpx.AsyncClient, args: AlertManagementInput) -> Any:
        if not args.mute_timing_name:
            raise ValueError("Mute timing name is required for delete_mute_timing action")

        response = await client.delete(f"/v1/provisioning/mute-timings/{args.mute_timing_name}")
        response.raise_for_status()
        return {"status": "success", "message": "Mute timing deleted"}

    @classmethod
    def _from_config(cls, config: Config) -> "AlertManagementTool":
        return cls(config)


#########################
# Annotation Management #
#########################


class AnnotationAction(str, Enum):
    """Possible annotation actions"""

    GET = "get"
    CREATE = "create"
    UPDATE = "update"
    DELETE = "delete"


class AnnotationManagementInput(BaseModel):
    """Input for annotation management operations"""

    action: AnnotationAction = Field(description="The annotation action to perform")

    # Get parameters
    from_time: Optional[int] = Field(
        default=None, description="Find annotations created after specific epoch datetime in milliseconds"
    )
    to_time: Optional[int] = Field(
        default=None, description="Find annotations created before specific epoch datetime in milliseconds"
    )
    limit: Optional[int] = Field(default=None, description="Max limit for results returned")
    tags: Optional[List[str]] = Field(default=None, description="List of tags to filter annotations by")
    type: Optional[str] = Field(default=None, description="Return alerts or user created annotations")
    dashboard_id: Optional[int] = Field(
        default=None, description="Find annotations that are scoped to a specific dashboard"
    )
    dashboard_uid: Optional[str] = Field(
        default=None, description="Find annotations that are scoped to a specific dashboard"
    )
    panel_id: Optional[int] = Field(default=None, description="Find annotations that are scoped to a specific panel")
    user_id: Optional[int] = Field(default=None, description="Limit response to annotations created by specific user")
    alert_id: Optional[int] = Field(default=None, description="Find annotations for a specified alert rule")

    # Create parameters
    time: Optional[int] = Field(default=None, description="Time in epoch milliseconds")
    time_end: Optional[int] = Field(default=None, description="End time for region annotation (optional)")
    text: Optional[str] = Field(default=None, description="Text content of the annotation")

    # Update/delete parameters
    annotation_id: Optional[str] = Field(default=None, description="ID of the annotation to update or delete")


class AnnotationManagementTool(BaseTool):
    """Tool for managing Grafana annotations"""

    _description = """Perform various operations related to Grafana annotations including:
    - get: Get annotations with filtering options
    - create: Create a new annotation
    - update: Update an existing annotation
    - delete: Delete an annotation
    """

    def __init__(self, config: Config) -> None:
        super().__init__(config=config, input_model=AnnotationManagementInput, description=self._description)

    async def run(self, args: AnnotationManagementInput, cancellation_token: CancellationToken) -> Any:
        async with get_http_client(self.config, cancellation_token) as client:
            # Handle different annotation operations based on the action
            if args.action == AnnotationAction.GET:
                return await self._get_annotations(client, args)
            elif args.action == AnnotationAction.CREATE:
                return await self._create_annotation(client, args)
            elif args.action == AnnotationAction.UPDATE:
                return await self._update_annotation(client, args)
            elif args.action == AnnotationAction.DELETE:
                return await self._delete_annotation(client, args)
            else:
                raise ValueError(f"Unsupported annotation action: {args.action}")

    async def _get_annotations(self, client: httpx.AsyncClient, args: AnnotationManagementInput) -> Any:
        params = {}

        if args.from_time is not None:
            params["from"] = args.from_time
        if args.to_time is not None:
            params["to"] = args.to_time
        if args.limit:
            params["limit"] = args.limit
        if args.tags:
            params["tags"] = args.tags
        if args.type:
            params["type"] = args.type
        if args.dashboard_id:
            params["dashboardId"] = args.dashboard_id
        if args.dashboard_uid:
            params["dashboardUID"] = args.dashboard_uid
        if args.panel_id:
            params["panelId"] = args.panel_id
        if args.user_id:
            params["userId"] = args.user_id
        if args.alert_id:
            params["alertId"] = args.alert_id

        response = await client.get("/annotations", params=params)
        response.raise_for_status()
        return response.json()

    async def _create_annotation(self, client: httpx.AsyncClient, args: AnnotationManagementInput) -> Any:
        if not args.time or not args.text:
            raise ValueError("Time and text are required for create annotation action")

        payload = {"time": args.time, "text": args.text}

        if args.dashboard_uid:
            payload["dashboardUID"] = args.dashboard_uid
        if args.panel_id:
            payload["panelId"] = args.panel_id
        if args.time_end:
            payload["timeEnd"] = args.time_end
        if args.tags:
            payload["tags"] = args.tags

        response = await client.post("/annotations", json=payload)
        response.raise_for_status()
        return response.json()

    async def _update_annotation(self, client: httpx.AsyncClient, args: AnnotationManagementInput) -> Any:
        if not args.annotation_id:
            raise ValueError("Annotation ID is required for update annotation action")

        payload = {}

        if args.time:
            payload["time"] = args.time
        if args.time_end:
            payload["timeEnd"] = args.time_end
        if args.tags:
            payload["tags"] = args.tags
        if args.text:
            payload["text"] = args.text

        response = await client.put(f"/annotations/{args.annotation_id}", json=payload)
        response.raise_for_status()
        return response.json()

    async def _delete_annotation(self, client: httpx.AsyncClient, args: AnnotationManagementInput) -> Any:
        if not args.annotation_id:
            raise ValueError("Annotation ID is required for delete annotation action")

        response = await client.delete(f"/annotations/{args.annotation_id}")
        response.raise_for_status()
        return response.json()

    @classmethod
    def _from_config(cls, config: Config) -> "AnnotationManagementTool":
        return cls(config)


#########################
# Miscellaneous Tools #
#########################


class MiscAction(str, Enum):
    """Possible miscellaneous actions"""

    GET_HEALTH = "get_health"
    CREATE_SNAPSHOT = "create_snapshot"
    GET_SNAPSHOT = "get_snapshot"
    DELETE_SNAPSHOT = "delete_snapshot"
    GET_PLAYLISTS = "get_playlists"
    GET_PLAYLIST = "get_playlist"
    CREATE_PLAYLIST = "create_playlist"
    UPDATE_PLAYLIST = "update_playlist"
    DELETE_PLAYLIST = "delete_playlist"


class MiscManagementInput(BaseModel):
    """Input for miscellaneous Grafana operations"""

    action: MiscAction = Field(description="The miscellaneous action to perform")

    # Snapshot parameters
    dashboard: Optional[Dict[str, Any]] = Field(default=None, description="Complete dashboard model JSON")
    name: Optional[str] = Field(default=None, description="Name of the snapshot or playlist")
    expires: Optional[int] = Field(default=None, description="Expiration time in seconds (0 for never)")
    external: Optional[bool] = Field(default=None, description="True to create an external snapshot")
    key: Optional[str] = Field(default=None, description="Snapshot key for retrieval or deletion")

    # Playlist parameters
    uid: Optional[str] = Field(default=None, description="Playlist UID")
    interval: Optional[str] = Field(default=None, description="Interval between dashboard transitions")
    items: Optional[List[Dict[str, Any]]] = Field(default=None, description="Dashboards in the playlist")

    # List parameters
    query: Optional[str] = Field(default=None, description="Search query")
    limit: Optional[int] = Field(default=None, description="Maximum number of results to return")


class MiscManagementTool(BaseTool):
    """Tool for miscellaneous Grafana operations"""

    _description = """Perform various miscellaneous operations in Grafana including:
    - get_health: Check Grafana health status
    - create_snapshot: Create a dashboard snapshot
    - get_snapshot: Get a dashboard snapshot
    - delete_snapshot: Delete a dashboard snapshot
    - get_playlists: Get all playlists
    - get_playlist: Get a specific playlist
    - create_playlist: Create a new playlist
    - update_playlist: Update an existing playlist
    - delete_playlist: Delete a playlist
    """

    def __init__(self, config: Config) -> None:
        super().__init__(config=config, input_model=MiscManagementInput, description=self._description)

    async def run(self, args: MiscManagementInput, cancellation_token: CancellationToken) -> Any:
        async with get_http_client(self.config, cancellation_token) as client:
            # Handle different miscellaneous operations based on the action
            if args.action == MiscAction.GET_HEALTH:
                return await self._get_health(client, args)
            elif args.action == MiscAction.CREATE_SNAPSHOT:
                return await self._create_snapshot(client, args)
            elif args.action == MiscAction.GET_SNAPSHOT:
                return await self._get_snapshot(client, args)
            elif args.action == MiscAction.DELETE_SNAPSHOT:
                return await self._delete_snapshot(client, args)
            elif args.action == MiscAction.GET_PLAYLISTS:
                return await self._get_playlists(client, args)
            elif args.action == MiscAction.GET_PLAYLIST:
                return await self._get_playlist(client, args)
            elif args.action == MiscAction.CREATE_PLAYLIST:
                return await self._create_playlist(client, args)
            elif args.action == MiscAction.UPDATE_PLAYLIST:
                return await self._update_playlist(client, args)
            elif args.action == MiscAction.DELETE_PLAYLIST:
                return await self._delete_playlist(client, args)
            else:
                raise ValueError(f"Unsupported miscellaneous action: {args.action}")

    async def _get_health(self, client: httpx.AsyncClient, args: MiscManagementInput) -> Any:
        response = await client.get("/health")
        response.raise_for_status()
        return response.json()

    async def _create_snapshot(self, client: httpx.AsyncClient, args: MiscManagementInput) -> Any:
        if not args.dashboard:
            raise ValueError("Dashboard JSON model is required for create_snapshot action")

        payload = {"dashboard": args.dashboard}

        if args.name:
            payload["name"] = args.name
        if args.expires is not None:
            payload["expires"] = args.expires
        if args.external is not None:
            payload["external"] = args.external
        if args.key:
            payload["key"] = args.key

        response = await client.post("/snapshots", json=payload)
        response.raise_for_status()
        return response.json()

    async def _get_snapshot(self, client: httpx.AsyncClient, args: MiscManagementInput) -> Any:
        if not args.key:
            raise ValueError("Snapshot key is required for get_snapshot action")

        response = await client.get(f"/snapshots/{args.key}")
        response.raise_for_status()
        return response.json()

    async def _delete_snapshot(self, client: httpx.AsyncClient, args: MiscManagementInput) -> Any:
        if not args.key:
            raise ValueError("Snapshot key is required for delete_snapshot action")

        response = await client.delete(f"/snapshots/{args.key}")
        response.raise_for_status()
        return response.json()

    async def _get_playlists(self, client: httpx.AsyncClient, args: MiscManagementInput) -> Any:
        params = {}

        if args.query:
            params["query"] = args.query
        if args.limit:
            params["limit"] = args.limit

        response = await client.get("/playlists", params=params)
        response.raise_for_status()
        return response.json()

    async def _get_playlist(self, client: httpx.AsyncClient, args: MiscManagementInput) -> Any:
        if not args.uid:
            raise ValueError("Playlist UID is required for get_playlist action")

        response = await client.get(f"/playlists/{args.uid}")
        response.raise_for_status()
        return response.json()

    async def _create_playlist(self, client: httpx.AsyncClient, args: MiscManagementInput) -> Any:
        if not args.name or not args.interval or not args.items:
            raise ValueError("Name, interval, and items are required for create_playlist action")

        payload = {"name": args.name, "interval": args.interval, "items": args.items}

        response = await client.post("/playlists", json=payload)
        response.raise_for_status()
        return response.json()

    async def _update_playlist(self, client: httpx.AsyncClient, args: MiscManagementInput) -> Any:
        if not args.uid or not args.name or not args.interval or not args.items:
            raise ValueError("UID, name, interval, and items are required for update_playlist action")

        payload = {"name": args.name, "interval": args.interval, "items": args.items}

        response = await client.put(f"/playlists/{args.uid}", json=payload)
        response.raise_for_status()
        return response.json()

    async def _delete_playlist(self, client: httpx.AsyncClient, args: MiscManagementInput) -> Any:
        if not args.uid:
            raise ValueError("Playlist UID is required for delete_playlist action")

        response = await client.delete(f"/playlists/{args.uid}")
        response.raise_for_status()
        return response.json()

    @classmethod
    def _from_config(cls, config: Config) -> "MiscManagementTool":
        return cls(config)


class DashboardAction(str, Enum):
    """Possible dashboard actions"""

    SEARCH = "search"
    GET = "get"
    CREATE = "create"
    UPDATE = "update"
    DELETE = "delete"
    GET_VERSIONS = "get_versions"
    GET_VERSION = "get_version"
    RESTORE_VERSION = "restore_version"
    GET_PERMISSIONS = "get_permissions"
    UPDATE_PERMISSIONS = "update_permissions"
    CALCULATE_DIFF = "calculate_diff"


class DashboardManagementInput(BaseModel):
    """Input for dashboard management operations"""

    action: DashboardAction = Field(description="The dashboard action to perform")

    # Search parameters
    query: Optional[str] = Field(default=None, description="Search query for dashboards")
    tag: Optional[List[str]] = Field(default=None, description="List of tags to search for")
    folder_uids: Optional[List[str]] = Field(default=None, description="List of folder UIDs to search in")
    starred: Optional[bool] = Field(
        default=None, description="Flag indicating if only starred Dashboards should be returned"
    )
    limit: Optional[int] = Field(default=None, description="Limit the number of returned results")

    # Get, update, delete parameters
    uid: Optional[str] = Field(default=None, description="Dashboard UID")

    # Create/update parameters
    dashboard: Optional[Dict[str, Any]] = Field(default=None, description="Complete dashboard model JSON")
    folder_uid: Optional[str] = Field(default=None, description="The folder to save the dashboard in")
    overwrite: Optional[bool] = Field(
        default=None, description="Whether to overwrite existing dashboard with same name"
    )
    message: Optional[str] = Field(default=None, description="A description of the changes made")

    # Version parameters
    version_id: Optional[int] = Field(default=None, description="Version ID to retrieve or restore to")

    # Diff parameters
    base_uid: Optional[str] = Field(default=None, description="Base dashboard UID for diff calculation")
    base_version: Optional[int] = Field(default=None, description="Version of base dashboard for diff")
    new_uid: Optional[str] = Field(default=None, description="New dashboard UID for diff calculation")
    new_version: Optional[int] = Field(default=None, description="Version of new dashboard for diff")
    diff_type: Optional[str] = Field(default=None, description="Type of diff to return (basic or json)")

    # Permissions parameters
    permissions: Optional[List[Dict[str, Any]]] = Field(default=None, description="List of permission items to set")


class DashboardManagementTool(BaseTool):
    """Tool for managing Grafana dashboards"""

    _description = """Perform various operations on Grafana dashboards including:
    - search: Search for dashboards with filtering and pagination
    - get: Retrieve a specific dashboard by UID
    - create/update: Create a new dashboard or update an existing one
    - delete: Delete a dashboard by UID
    - get_versions: List all versions of a dashboard
    - get_version: Retrieve a specific version of a dashboard
    - restore_version: Restore a dashboard to a previous version
    - get_permissions: Get dashboard permissions
    - update_permissions: Update dashboard permissions
    - calculate_diff: Calculate difference between dashboard versions
    """

    def __init__(self, config: Config) -> None:
        super().__init__(config=config, input_model=DashboardManagementInput, description=self._description)

    async def run(self, args: DashboardManagementInput, cancellation_token: CancellationToken) -> Any:
        async with get_http_client(self.config, cancellation_token) as client:
            # Handle different dashboard operations based on the action
            if args.action == DashboardAction.SEARCH:
                return await self._search_dashboards(client, args)
            elif args.action == DashboardAction.GET:
                return await self._get_dashboard(client, args)
            elif args.action in [DashboardAction.CREATE, DashboardAction.UPDATE]:
                return await self._create_update_dashboard(client, args)
            elif args.action == DashboardAction.DELETE:
                return await self._delete_dashboard(client, args)
            elif args.action == DashboardAction.GET_VERSIONS:
                return await self._get_dashboard_versions(client, args)
            elif args.action == DashboardAction.GET_VERSION:
                return await self._get_dashboard_version(client, args)
            elif args.action == DashboardAction.RESTORE_VERSION:
                return await self._restore_dashboard_version(client, args)
            elif args.action == DashboardAction.GET_PERMISSIONS:
                return await self._get_dashboard_permissions(client, args)
            elif args.action == DashboardAction.UPDATE_PERMISSIONS:
                return await self._update_dashboard_permissions(client, args)
            elif args.action == DashboardAction.CALCULATE_DIFF:
                return await self._calculate_dashboard_diff(client, args)
            else:
                raise ValueError(f"Unsupported dashboard action: {args.action}")

    async def _search_dashboards(self, client: httpx.AsyncClient, args: DashboardManagementInput) -> Any:
        params = {}

        if args.query:
            params["query"] = args.query
        if args.tag:
            params["tag"] = args.tag
        if args.folder_uids:
            params["folderUIDs"] = args.folder_uids
        if args.starred is not None:
            params["starred"] = str(args.starred).lower()
        if args.limit:
            params["limit"] = args.limit

        response = await client.get("/search", params=params)
        response.raise_for_status()
        return response.json()

    async def _get_dashboard(self, client: httpx.AsyncClient, args: DashboardManagementInput) -> Any:
        if not args.uid:
            raise ValueError("Dashboard UID is required for get action")

        response = await client.get(f"/dashboards/uid/{args.uid}")
        response.raise_for_status()
        return response.json()

    async def _create_update_dashboard(self, client: httpx.AsyncClient, args: DashboardManagementInput) -> Any:
        if not args.dashboard:
            raise ValueError("Dashboard JSON model is required for create/update action")

        payload = {"dashboard": args.dashboard, "overwrite": args.overwrite or False}

        if args.folder_uid:
            payload["folderUid"] = args.folder_uid
        if args.message:
            payload["message"] = args.message

        response = await client.post("/dashboards/db", json=payload)
        response.raise_for_status()
        return response.json()

    async def _delete_dashboard(self, client: httpx.AsyncClient, args: DashboardManagementInput) -> Any:
        if not args.uid:
            raise ValueError("Dashboard UID is required for delete action")

        response = await client.delete(f"/dashboards/uid/{args.uid}")
        response.raise_for_status()
        return response.json()

    async def _get_dashboard_versions(self, client: httpx.AsyncClient, args: DashboardManagementInput) -> Any:
        if not args.uid:
            raise ValueError("Dashboard UID is required for get_versions action")

        params = {}
        if args.limit:
            params["limit"] = args.limit

        response = await client.get(f"/dashboards/uid/{args.uid}/versions", params=params)
        response.raise_for_status()
        return response.json()

    async def _get_dashboard_version(self, client: httpx.AsyncClient, args: DashboardManagementInput) -> Any:
        if not args.uid or not args.version_id:
            raise ValueError("Dashboard UID and version_id are required for get_version action")

        response = await client.get(f"/dashboards/uid/{args.uid}/versions/{args.version_id}")
        response.raise_for_status()
        return response.json()

    async def _restore_dashboard_version(self, client: httpx.AsyncClient, args: DashboardManagementInput) -> Any:
        if not args.uid or not args.version_id:
            raise ValueError("Dashboard UID and version_id are required for restore_version action")

        payload = {"version": args.version_id}

        response = await client.post(f"/dashboards/uid/{args.uid}/restore", json=payload)
        response.raise_for_status()
        return response.json()

    async def _get_dashboard_permissions(self, client: httpx.AsyncClient, args: DashboardManagementInput) -> Any:
        if not args.uid:
            raise ValueError("Dashboard UID is required for get_permissions action")

        response = await client.get(f"/dashboards/uid/{args.uid}/permissions")
        response.raise_for_status()
        return response.json()

    async def _update_dashboard_permissions(self, client: httpx.AsyncClient, args: DashboardManagementInput) -> Any:
        if not args.uid or not args.permissions:
            raise ValueError("Dashboard UID and permissions are required for update_permissions action")

        response = await client.post(f"/dashboards/uid/{args.uid}/permissions", json={"items": args.permissions})
        response.raise_for_status()
        return response.json()

    async def _calculate_dashboard_diff(self, client: httpx.AsyncClient, args: DashboardManagementInput) -> Any:
        if not args.base_uid or not args.new_uid:
            raise ValueError("base_uid and new_uid are required for calculate_diff action")

        payload = {
            "base": {
                "dashboardId": args.base_uid,
            },
            "new": {
                "dashboardId": args.new_uid,
            },
            "diffType": args.diff_type or "basic",
        }

        if args.base_version:
            payload["base"]["version"] = args.base_version
        if args.new_version:
            payload["new"]["version"] = args.new_version

        response = await client.post("/dashboards/calculate-diff", json=payload)
        response.raise_for_status()
        return response.json()

    @classmethod
    def _from_config(cls, config: Config) -> "DashboardManagementTool":
        return cls(config)


#######################
# Folder Management #
#######################


class FolderAction(str, Enum):
    """Possible folder actions"""

    LIST = "list"
    GET = "get"
    CREATE = "create"
    UPDATE = "update"
    DELETE = "delete"
    GET_PERMISSIONS = "get_permissions"
    UPDATE_PERMISSIONS = "update_permissions"


class FolderManagementInput(BaseModel):
    """Input for folder management operations"""

    action: FolderAction = Field(description="The folder action to perform")

    # List parameters
    limit: Optional[int] = Field(default=None, description="Limit the maximum number of folders to return")
    page: Optional[int] = Field(default=None, description="Page index for starting fetching folders")
    parent_uid: Optional[str] = Field(default=None, description="The parent folder UID")
    permission: Optional[str] = Field(
        default=None, description="Set to `Edit` to return folders that the user can edit"
    )

    # Get, update, delete parameters
    uid: Optional[str] = Field(default=None, description="Folder UID")

    # Create/update parameters
    title: Optional[str] = Field(default=None, description="Folder title")
    version: Optional[int] = Field(default=None, description="Current version of the folder")
    overwrite: Optional[bool] = Field(
        default=None, description="Whether to overwrite another folder with the same name"
    )

    # Delete parameters
    force_delete_rules: Optional[bool] = Field(
        default=None, description="Whether to force deletion of alert rules under the folder"
    )

    # Permissions parameters
    permissions: Optional[List[Dict[str, Any]]] = Field(default=None, description="List of permission items to set")


class FolderManagementTool(BaseTool):
    """Tool for managing Grafana folders"""

    _description = """Perform various operations on Grafana folders including:
    - list: Get all folders with filtering and pagination
    - get: Retrieve a specific folder by UID
    - create: Create a new folder
    - update: Update an existing folder
    - delete: Delete a folder by UID
    - get_permissions: Get folder permissions
    - update_permissions: Update folder permissions
    """

    def __init__(self, config: Config) -> None:
        super().__init__(config=config, input_model=FolderManagementInput, description=self._description)

    async def run(self, args: FolderManagementInput, cancellation_token: CancellationToken) -> Any:
        async with get_http_client(self.config, cancellation_token) as client:
            # Handle different folder operations based on the action
            if args.action == FolderAction.LIST:
                return await self._list_folders(client, args)
            elif args.action == FolderAction.GET:
                return await self._get_folder(client, args)
            elif args.action == FolderAction.CREATE:
                return await self._create_folder(client, args)
            elif args.action == FolderAction.UPDATE:
                return await self._update_folder(client, args)
            elif args.action == FolderAction.DELETE:
                return await self._delete_folder(client, args)
            elif args.action == FolderAction.GET_PERMISSIONS:
                return await self._get_folder_permissions(client, args)
            elif args.action == FolderAction.UPDATE_PERMISSIONS:
                return await self._update_folder_permissions(client, args)
            else:
                raise ValueError(f"Unsupported folder action: {args.action}")

    async def _list_folders(self, client: httpx.AsyncClient, args: FolderManagementInput) -> Any:
        params = {}

        if args.limit:
            params["limit"] = args.limit
        if args.page:
            params["page"] = args.page
        if args.parent_uid:
            params["parentUid"] = args.parent_uid
        if args.permission:
            params["permission"] = args.permission

        response = await client.get("/folders", params=params)
        response.raise_for_status()
        return response.json()

    async def _get_folder(self, client: httpx.AsyncClient, args: FolderManagementInput) -> Any:
        if not args.uid:
            raise ValueError("Folder UID is required for get action")

        response = await client.get(f"/folders/{args.uid}")
        response.raise_for_status()
        return response.json()

    async def _create_folder(self, client: httpx.AsyncClient, args: FolderManagementInput) -> Any:
        if not args.title:
            raise ValueError("Folder title is required for create action")

        payload = {"title": args.title}

        if args.uid:
            payload["uid"] = args.uid
        if args.parent_uid:
            payload["parentUid"] = args.parent_uid

        response = await client.post("/folders", json=payload)
        response.raise_for_status()
        return response.json()

    async def _update_folder(self, client: httpx.AsyncClient, args: FolderManagementInput) -> Any:
        if not args.uid or not args.title:
            raise ValueError("Folder UID and title are required for update action")

        payload = {"title": args.title, "overwrite": args.overwrite or False}

        if args.version:
            payload["version"] = args.version

        response = await client.put(f"/folders/{args.uid}", json=payload)
        response.raise_for_status()
        return response.json()

    async def _delete_folder(self, client: httpx.AsyncClient, args: FolderManagementInput) -> Any:
        if not args.uid:
            raise ValueError("Folder UID is required for delete action")

        params = {}
        if args.force_delete_rules:
            params["forceDeleteRules"] = "true"

        response = await client.delete(f"/folders/{args.uid}", params=params)
        response.raise_for_status()
        return response.json()

    async def _get_folder_permissions(self, client: httpx.AsyncClient, args: FolderManagementInput) -> Any:
        if not args.uid:
            raise ValueError("Folder UID is required for get_permissions action")

        response = await client.get(f"/folders/{args.uid}/permissions")
        response.raise_for_status()
        return response.json()

    async def _update_folder_permissions(self, client: httpx.AsyncClient, args: FolderManagementInput) -> Any:
        if not args.uid or not args.permissions:
            raise ValueError("Folder UID and permissions are required for update_permissions action")

        response = await client.post(f"/folders/{args.uid}/permissions", json={"items": args.permissions})
        response.raise_for_status()
        return response.json()

    @classmethod
    def _from_config(cls, config: Config) -> "FolderManagementTool":
        return cls(config)


#########################
# DataSource Management #
#########################


class DataSourceAction(str, Enum):
    """Possible data source actions"""

    LIST = "list"
    GET = "get"
    GET_BY_NAME = "get_by_name"
    CREATE = "create"
    UPDATE = "update"
    DELETE = "delete"
    TEST = "test"
    QUERY = "query"


class DataSourceManagementInput(BaseModel):
    """Input for data source management operations"""

    action: DataSourceAction = Field(description="The data source action to perform")

    # Get, update, delete, test parameters
    uid: Optional[str] = Field(default=None, description="Data source UID")

    # Get by name parameter
    name: Optional[str] = Field(default=None, description="Data source name")

    # Create/update parameters
    type: Optional[str] = Field(default=None, description="Type of data source (e.g., 'prometheus', 'mysql')")
    url: Optional[str] = Field(default=None, description="URL of the data source")
    access: Optional[str] = Field(default=None, description="Access mode (direct or proxy)")
    basic_auth: Optional[bool] = Field(default=None, description="Enable basic authentication")
    basic_auth_user: Optional[str] = Field(default=None, description="Basic auth username")
    basic_auth_password: Optional[str] = Field(default=None, description="Basic auth password")
    json_data: Optional[Dict[str, Any]] = Field(default=None, description="Type-specific JSON data configuration")
    secure_json_data: Optional[Dict[str, Any]] = Field(default=None, description="Secure JSON data (will be encrypted)")

    # Query parameters
    query_type: Optional[str] = Field(default=None, description="Type of query to execute")
    target: Optional[Dict[str, Any]] = Field(default=None, description="The query target parameters")
    max_data_points: Optional[int] = Field(default=None, description="Maximum number of data points to return")
    time_range: Optional[Dict[str, str]] = Field(default=None, description="Time range for the query (from, to)")


class DataSourceManagementTool(BaseTool):
    """Tool for managing Grafana data sources"""

    _description = """Perform various operations on Grafana data sources including:
    - list: Get all data sources
    - get: Retrieve a specific data source by UID
    - get_by_name: Retrieve a specific data source by name
    - create: Create a new data source
    - update: Update an existing data source
    - delete: Delete a data source by UID
    - test: Test a data source connection
    - query: Execute a query against a data source
    """

    def __init__(self, config: Config) -> None:
        super().__init__(config=config, input_model=DataSourceManagementInput, description=self._description)

    async def run(self, args: DataSourceManagementInput, cancellation_token: CancellationToken) -> Any:
        async with get_http_client(self.config, cancellation_token) as client:
            # Handle different data source operations based on the action
            if args.action == DataSourceAction.LIST:
                return await self._list_datasources(client, args)
            elif args.action == DataSourceAction.GET:
                return await self._get_datasource(client, args)
            elif args.action == DataSourceAction.GET_BY_NAME:
                return await self._get_datasource_by_name(client, args)
            elif args.action == DataSourceAction.CREATE:
                return await self._create_datasource(client, args)
            elif args.action == DataSourceAction.UPDATE:
                return await self._update_datasource(client, args)
            elif args.action == DataSourceAction.DELETE:
                return await self._delete_datasource(client, args)
            elif args.action == DataSourceAction.TEST:
                return await self._test_datasource(client, args)
            elif args.action == DataSourceAction.QUERY:
                return await self._query_datasource(client, args)
            else:
                raise ValueError(f"Unsupported data source action: {args.action}")

    async def _list_datasources(self, client: httpx.AsyncClient, args: DataSourceManagementInput) -> Any:
        response = await client.get("/datasources")
        response.raise_for_status()
        return response.json()

    async def _get_datasource(self, client: httpx.AsyncClient, args: DataSourceManagementInput) -> Any:
        if not args.uid:
            raise ValueError("Data source UID is required for get action")

        response = await client.get(f"/datasources/uid/{args.uid}")
        response.raise_for_status()
        return response.json()

    async def _get_datasource_by_name(self, client: httpx.AsyncClient, args: DataSourceManagementInput) -> Any:
        if not args.name:
            raise ValueError("Data source name is required for get_by_name action")

        response = await client.get(f"/datasources/name/{args.name}")
        response.raise_for_status()
        return response.json()

    async def _create_datasource(self, client: httpx.AsyncClient, args: DataSourceManagementInput) -> Any:
        if not args.name or not args.type or not args.url or not args.access:
            raise ValueError("Name, type, URL, and access mode are required for create action")

        payload = {"name": args.name, "type": args.type, "url": args.url, "access": args.access}

        if args.basic_auth is not None:
            payload["basicAuth"] = args.basic_auth
        if args.basic_auth_user:
            payload["basicAuthUser"] = args.basic_auth_user
        if args.basic_auth_password:
            payload["secureJsonData"] = payload.get("secureJsonData", {})
            payload["secureJsonData"]["basicAuthPassword"] = args.basic_auth_password
        if args.json_data:
            payload["jsonData"] = args.json_data
        if args.secure_json_data:
            payload["secureJsonData"] = payload.get("secureJsonData", {})
            payload["secureJsonData"].update(args.secure_json_data)
        if args.uid:
            payload["uid"] = args.uid

        response = await client.post("/datasources", json=payload)
        response.raise_for_status()
        return response.json()

    async def _update_datasource(self, client: httpx.AsyncClient, args: DataSourceManagementInput) -> Any:
        if not args.uid:
            raise ValueError("Data source UID is required for update action")

        # Get current data source to avoid replacing all fields
        current_ds_response = await client.get(f"/datasources/uid/{args.uid}")
        current_ds_response.raise_for_status()
        current_ds = current_ds_response.json()

        # Start with the current data source and update fields
        payload = current_ds

        if args.name:
            payload["name"] = args.name
        if args.type:
            payload["type"] = args.type
        if args.url:
            payload["url"] = args.url
        if args.access:
            payload["access"] = args.access
        if args.basic_auth is not None:
            payload["basicAuth"] = args.basic_auth
        if args.basic_auth_user:
            payload["basicAuthUser"] = args.basic_auth_user
        if args.basic_auth_password:
            payload["secureJsonData"] = payload.get("secureJsonData", {})
            payload["secureJsonData"]["basicAuthPassword"] = args.basic_auth_password
        if args.json_data:
            payload["jsonData"] = args.json_data
        if args.secure_json_data:
            payload["secureJsonData"] = payload.get("secureJsonData", {})
            payload["secureJsonData"].update(args.secure_json_data)

        response = await client.put(f"/datasources/uid/{args.uid}", json=payload)
        response.raise_for_status()
        return response.json()

    async def _delete_datasource(self, client: httpx.AsyncClient, args: DataSourceManagementInput) -> Any:
        if not args.uid:
            raise ValueError("Data source UID is required for delete action")

        response = await client.delete(f"/datasources/uid/{args.uid}")
        response.raise_for_status()
        return response.json()

    async def _test_datasource(self, client: httpx.AsyncClient, args: DataSourceManagementInput) -> Any:
        if not args.uid:
            raise ValueError("Data source UID is required for test action")

        response = await client.get(f"/datasources/uid/{args.uid}/health")
        response.raise_for_status()
        return response.json()

    async def _query_datasource(self, client: httpx.AsyncClient, args: DataSourceManagementInput) -> Any:
        if not args.uid or not args.query_type or not args.target:
            raise ValueError("Data source UID, query_type, and target are required for query action")

        payload = {"queries": [{"refId": "A", "datasourceUid": args.uid, "queryType": args.query_type, **args.target}]}

        if args.max_data_points:
            payload["queries"][0]["maxDataPoints"] = args.max_data_points

        if args.time_range:
            payload["range"] = args.time_range

        response = await client.post("/ds/query", json=payload)
        response.raise_for_status()
        return response.json()

    @classmethod
    def _from_config(cls, config: Config) -> "DataSourceManagementTool":
        return cls(config)


#######################
# User Management #
#######################


class UserAction(str, Enum):
    """Possible user actions"""

    GET_CURRENT = "get_current"
    UPDATE_CURRENT = "update_current"
    GET_ORGS = "get_orgs"
    SWITCH_ORG = "switch_org"
    GET_TEAMS = "get_teams"
    GET_PREFERENCES = "get_preferences"
    UPDATE_PREFERENCES = "update_preferences"
    LIST_USERS = "list_users"
    GET_USER = "get_user"
    CREATE_USER = "create_user"
    UPDATE_USER = "update_user"
    DELETE_USER = "delete_user"
    ENABLE_USER = "enable_user"
    DISABLE_USER = "disable_user"
    UPDATE_PASSWORD = "update_password"


class UserManagementInput(BaseModel):
    """Input for user management operations"""

    action: UserAction = Field(description="The user action to perform")

    # User ID for specific user operations
    user_id: Optional[int] = Field(default=None, description="User ID for operations on specific users")

    # Current user update parameters
    name: Optional[str] = Field(default=None, description="User display name")
    email: Optional[str] = Field(default=None, description="User email address")
    login: Optional[str] = Field(default=None, description="User login name")
    theme: Optional[str] = Field(default=None, description="User theme preference")

    # Organization switching
    org_id: Optional[int] = Field(default=None, description="Organization ID to switch to")

    # Preferences parameters
    home_dashboard_id: Optional[int] = Field(default=None, description="Home dashboard ID")
    home_dashboard_uid: Optional[str] = Field(default=None, description="Home dashboard UID")
    timezone: Optional[str] = Field(default=None, description="Timezone preference")

    # User creation/update parameters
    password: Optional[str] = Field(default=None, description="User password")

    # List users parameters
    query: Optional[str] = Field(default=None, description="Search query for users")
    perpage: Optional[int] = Field(default=None, description="Number of items per page")
    page: Optional[int] = Field(default=None, description="Page number")


class UserManagementTool(BaseTool):
    """Tool for managing Grafana users"""

    _description = """Perform various operations related to Grafana users including:
    - get_current: Get current authenticated user information
    - update_current: Update current authenticated user information
    - get_orgs: Get organizations for the current user
    - switch_org: Switch the current user to a different organization
    - get_teams: Get teams the current user belongs to
    - get_preferences: Get user preferences
    - update_preferences: Update user preferences
    - list_users: List/search all users (requires admin)
    - get_user: Get a specific user by ID (requires admin)
    - create_user: Create a new user (requires admin)
    - update_user: Update a user (requires admin)
    - delete_user: Delete a user (requires admin)
    - enable_user/disable_user: Enable or disable a user account (requires admin)
    - update_password: Update a user's password (requires admin)
    """

    def __init__(self, config) -> None:
        super().__init__(config=config, input_model=UserManagementInput, description=self._description)

    async def run(self, args: UserManagementInput, cancellation_token: CancellationToken) -> Any:
        async with get_http_client(self.config, cancellation_token) as client:
            # Handle different user operations based on the action
            if args.action == UserAction.GET_CURRENT:
                return await self._get_current_user(client, args)
            elif args.action == UserAction.UPDATE_CURRENT:
                return await self._update_current_user(client, args)
            elif args.action == UserAction.GET_ORGS:
                return await self._get_user_orgs(client, args)
            elif args.action == UserAction.SWITCH_ORG:
                return await self._switch_user_org(client, args)
            elif args.action == UserAction.GET_TEAMS:
                return await self._get_user_teams(client, args)
            elif args.action == UserAction.GET_PREFERENCES:
                return await self._get_user_preferences(client, args)
            elif args.action == UserAction.UPDATE_PREFERENCES:
                return await self._update_user_preferences(client, args)
            elif args.action == UserAction.LIST_USERS:
                return await self._list_users(client, args)
            elif args.action == UserAction.GET_USER:
                return await self._get_user(client, args)
            elif args.action == UserAction.CREATE_USER:
                return await self._create_user(client, args)
            elif args.action == UserAction.UPDATE_USER:
                return await self._update_user(client, args)
            elif args.action == UserAction.DELETE_USER:
                return await self._delete_user(client, args)
            elif args.action == UserAction.ENABLE_USER:
                return await self._enable_disable_user(client, args, enable=True)
            elif args.action == UserAction.DISABLE_USER:
                return await self._enable_disable_user(client, args, enable=False)
            elif args.action == UserAction.UPDATE_PASSWORD:
                return await self._update_password(client, args)
            else:
                raise ValueError(f"Unsupported user action: {args.action}")

    async def _get_current_user(self, client: httpx.AsyncClient, args: UserManagementInput) -> Any:
        response = await client.get("/user")
        response.raise_for_status()
        return response.json()

    async def _update_current_user(self, client: httpx.AsyncClient, args: UserManagementInput) -> Any:
        payload = {}

        if args.name:
            payload["name"] = args.name
        if args.email:
            payload["email"] = args.email
        if args.login:
            payload["login"] = args.login
        if args.theme:
            payload["theme"] = args.theme

        response = await client.put("/user", json=payload)
        response.raise_for_status()
        return response.json()

    async def _get_user_orgs(self, client: httpx.AsyncClient, args: UserManagementInput) -> Any:
        response = await client.get("/user/orgs")
        response.raise_for_status()
        return response.json()

    async def _switch_user_org(self, client: httpx.AsyncClient, args: UserManagementInput) -> Any:
        if not args.org_id:
            raise ValueError("Organization ID is required for switch_org action")

        response = await client.post(f"/user/using/{args.org_id}")
        response.raise_for_status()
        return response.json()

    async def _get_user_teams(self, client: httpx.AsyncClient, args: UserManagementInput) -> Any:
        response = await client.get("/user/teams")
        response.raise_for_status()
        return response.json()

    async def _get_user_preferences(self, client: httpx.AsyncClient, args: UserManagementInput) -> Any:
        response = await client.get("/user/preferences")
        response.raise_for_status()
        return response.json()

    async def _update_user_preferences(self, client: httpx.AsyncClient, args: UserManagementInput) -> Any:
        payload = {}

        if args.theme:
            payload["theme"] = args.theme
        if args.home_dashboard_id:
            payload["homeDashboardId"] = args.home_dashboard_id
        if args.home_dashboard_uid:
            payload["homeDashboardUID"] = args.home_dashboard_uid
        if args.timezone:
            payload["timezone"] = args.timezone

        response = await client.put("/user/preferences", json=payload)
        response.raise_for_status()
        return response.json()

    async def _list_users(self, client: httpx.AsyncClient, args: UserManagementInput) -> Any:
        params = {}

        if args.query:
            params["query"] = args.query
        if args.perpage:
            params["perpage"] = args.perpage
        if args.page:
            params["page"] = args.page

        response = await client.get("/users/search", params=params)
        response.raise_for_status()
        return response.json()

    async def _get_user(self, client: httpx.AsyncClient, args: UserManagementInput) -> Any:
        if not args.user_id:
            raise ValueError("User ID is required for get_user action")

        response = await client.get(f"/users/{args.user_id}")
        response.raise_for_status()
        return response.json()

    async def _create_user(self, client: httpx.AsyncClient, args: UserManagementInput) -> Any:
        if not args.name or not args.email or not args.login or not args.password:
            raise ValueError("Name, email, login, and password are required for create_user action")

        payload = {"name": args.name, "email": args.email, "login": args.login, "password": args.password}

        if args.org_id:
            payload["OrgId"] = args.org_id

        response = await client.post("/admin/users", json=payload)
        response.raise_for_status()
        return response.json()

    async def _update_user(self, client: httpx.AsyncClient, args: UserManagementInput) -> Any:
        if not args.user_id:
            raise ValueError("User ID is required for update_user action")

        payload = {}

        if args.name:
            payload["name"] = args.name
        if args.email:
            payload["email"] = args.email
        if args.login:
            payload["login"] = args.login
        if args.theme:
            payload["theme"] = args.theme

        response = await client.put(f"/users/{args.user_id}", json=payload)
        response.raise_for_status()
        return response.json()

    async def _delete_user(self, client: httpx.AsyncClient, args: UserManagementInput) -> Any:
        if not args.user_id:
            raise ValueError("User ID is required for delete_user action")

        response = await client.delete(f"/admin/users/{args.user_id}")
        response.raise_for_status()
        return response.json()

    async def _enable_disable_user(self, client: httpx.AsyncClient, args: UserManagementInput, enable: bool) -> Any:
        if not args.user_id:
            raise ValueError("User ID is required for enable/disable user action")

        endpoint = f"/admin/users/{args.user_id}/enable" if enable else f"/admin/users/{args.user_id}/disable"
        response = await client.post(endpoint)
        response.raise_for_status()
        return response.json()

    async def _update_password(self, client: httpx.AsyncClient, args: UserManagementInput) -> Any:
        if not args.user_id or not args.password:
            raise ValueError("User ID and password are required for update_password action")

        payload = {"password": args.password}

        response = await client.put(f"/admin/users/{args.user_id}/password", json=payload)
        response.raise_for_status()
        return response.json()

    @classmethod
    def _from_config(cls, config) -> "UserManagementTool":
        return cls(config)
