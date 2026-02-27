"use server";
import { revalidatePath } from "next/cache";
import { fetchApi, createErrorResponse } from "./utils";
import { BaseResponse, GitRepo, AddGitRepoRequest } from "@/types";

// The gitrepo-mcp service returns repos wrapped in { repos: [...] }
interface ListReposResponse {
  repos: GitRepo[];
}

export async function getGitRepos(): Promise<BaseResponse<GitRepo[]>> {
  try {
    const response = await fetchApi<ListReposResponse>("/gitrepos");

    if (!response) {
      throw new Error("Failed to get git repos");
    }

    const repos = response.repos || [];
    repos.sort((a, b) => a.name.localeCompare(b.name));

    return {
      message: "Git repos fetched successfully",
      data: repos,
    };
  } catch (error) {
    return createErrorResponse<GitRepo[]>(error, "Error getting git repos");
  }
}

export async function getGitRepo(name: string): Promise<BaseResponse<GitRepo>> {
  try {
    const response = await fetchApi<GitRepo>(`/gitrepos/${name}`);

    if (!response) {
      throw new Error("Failed to get git repo");
    }

    return {
      message: "Git repo fetched successfully",
      data: response,
    };
  } catch (error) {
    return createErrorResponse<GitRepo>(error, "Error getting git repo");
  }
}

export async function addGitRepo(req: AddGitRepoRequest): Promise<BaseResponse<GitRepo>> {
  try {
    const response = await fetchApi<GitRepo>("/gitrepos", {
      method: "POST",
      body: JSON.stringify(req),
    });

    if (!response) {
      throw new Error("Failed to add git repo");
    }

    revalidatePath("/git");

    return {
      message: "Git repo added successfully",
      data: response,
    };
  } catch (error) {
    return createErrorResponse<GitRepo>(error, "Error adding git repo");
  }
}

export async function deleteGitRepo(name: string): Promise<BaseResponse<void>> {
  try {
    await fetchApi(`/gitrepos/${name}`, {
      method: "DELETE",
    });

    revalidatePath("/git");
    return { message: "Git repo deleted successfully" };
  } catch (error) {
    return createErrorResponse<void>(error, "Error deleting git repo");
  }
}

export async function syncGitRepo(name: string): Promise<BaseResponse<GitRepo>> {
  try {
    const response = await fetchApi<GitRepo>(`/gitrepos/${name}/sync`, {
      method: "POST",
    });

    if (!response) {
      throw new Error("Failed to sync git repo");
    }

    revalidatePath("/git");

    return {
      message: "Git repo synced successfully",
      data: response,
    };
  } catch (error) {
    return createErrorResponse<GitRepo>(error, "Error syncing git repo");
  }
}

export async function indexGitRepo(name: string): Promise<BaseResponse<GitRepo>> {
  try {
    const response = await fetchApi<GitRepo>(`/gitrepos/${name}/index`, {
      method: "POST",
    });

    if (!response) {
      throw new Error("Failed to index git repo");
    }

    revalidatePath("/git");

    return {
      message: "Git repo indexing started",
      data: response,
    };
  } catch (error) {
    return createErrorResponse<GitRepo>(error, "Error indexing git repo");
  }
}
